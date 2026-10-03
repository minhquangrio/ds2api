package extprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"api.openai.com", "https://api.openai.com/v1"},
		{"https://api.openai.com/", "https://api.openai.com/v1"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"http://localhost:8000/v1/models", "http://localhost:8000/v1"},
		{"https://myproxy.com/prefix/v1/responses?key=val#frag", "https://myproxy.com/prefix/v1"},
		{"", ""},
	}

	for _, c := range cases {
		got := NormalizeBaseURL(c.in)
		if got != c.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRedactToken(t *testing.T) {
	token := "sk-secret-token-12345"
	err := errors.New("upstream error: failed for sk-secret-token-12345 unauthorized")
	redacted := RedactToken(err, token)
	if strings.Contains(redacted.Error(), token) {
		t.Fatalf("token was not redacted: %v", redacted)
	}
	if !strings.Contains(redacted.Error(), "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in error: %v", redacted)
	}
}

func TestStoreCRUD(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DS2API_EXTERNAL_PROVIDERS_PATH", filepath.Join(tmpDir, "providers.json"))

	p := Provider{
		ID:      "p_test",
		Name:    "Test Provider",
		BaseURL: "https://api.example.com/v1",
		Token:   "secret_key",
		Models: []ProviderModel{
			{ID: "model-1", Name: "Model One"},
		},
	}

	if err := SaveProvider(p); err != nil {
		t.Fatalf("SaveProvider failed: %v", err)
	}

	list, err := ListProviders()
	if err != nil {
		t.Fatalf("ListProviders failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != "p_test" {
		t.Fatalf("unexpected list: %+v", list)
	}

	fetched, err := GetProvider("p_test")
	if err != nil || fetched.Name != "Test Provider" {
		t.Fatalf("GetProvider failed: %v, fetched: %+v", err, fetched)
	}

	// Update models
	newModels := []ProviderModel{
		{ID: "model-1", Capabilities: []string{"tools"}},
		{ID: "model-2"},
	}
	if err := UpdateProviderModels("p_test", newModels, "connected"); err != nil {
		t.Fatalf("UpdateProviderModels failed: %v", err)
	}

	updated, _ := GetProvider("p_test")
	if len(updated.Models) != 2 || updated.ConnectionStatus != "connected" {
		t.Fatalf("unexpected updated provider: %+v", updated)
	}

	if err := DeleteProvider("p_test"); err != nil {
		t.Fatalf("DeleteProvider failed: %v", err)
	}

	emptyList, _ := ListProviders()
	if len(emptyList) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(emptyList))
	}
}

func TestDiscoverModelsMocked(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-4o", "context_window": 128000},
					{"id": "gpt-4o-mini", "name": "GPT-4o Mini"},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	ctx := context.Background()
	models, err := DiscoverModels(ctx, ts.URL+"/v1", "test_token")
	if err != nil {
		t.Fatalf("DiscoverModels failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4o" || models[0].ContextWindow != 128000 {
		t.Fatalf("unexpected model: %+v", models[0])
	}
}

func TestInspectModelMocked(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)

			// A real tool call must come back for the tools capability to be
			// recorded; a bare 200 is not evidence of tool support.
			if _, hasTools := body["tools"]; hasTools {
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{
						map[string]any{
							"message": map[string]any{
								"role": "assistant",
								"tool_calls": []any{
									map[string]any{
										"id":   "call_1",
										"type": "function",
										"function": map[string]any{
											"name":      "provider_probe",
											"arguments": `{"query":"ok"}`,
										},
									},
								},
							},
						},
					},
				})
				return
			}

			// If reasoning_effort passed
			if _, hasEffort := body["reasoning_effort"]; hasEffort {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": "unsupported parameter: reasoning_effort"}`))
				return
			}

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "OK"}}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	ctx := context.Background()
	m := ProviderModel{ID: "test-model"}
	inspected := InspectModel(ctx, ts.URL+"/v1", "token", m)

	if inspected.InspectStatus != "ready" {
		t.Fatalf("expected status ready, got %s", inspected.InspectStatus)
	}

	hasTools := false
	for _, c := range inspected.Capabilities {
		if c == "tools" {
			hasTools = true
		}
		if c == "reasoning" {
			t.Errorf("reasoning should not have passed")
		}
	}
	if !hasTools {
		t.Errorf("expected tools capability to be detected")
	}

	// Test preservation on temporary failure (e.g. 429 busy)
	tsBusy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": "rate limit exceeded"}`))
	}))
	defer tsBusy.Close()

	busyInspected := InspectModel(ctx, tsBusy.URL+"/v1", "token", inspected)
	if busyInspected.InspectStatus != "busy" {
		t.Fatalf("expected status busy, got %s", busyInspected.InspectStatus)
	}
	if busyInspected.CapabilitiesSource != "last_successful" {
		t.Errorf("expected capabilities_source last_successful, got %s", busyInspected.CapabilitiesSource)
	}
	if len(busyInspected.Capabilities) != len(inspected.Capabilities) {
		t.Errorf("expected capabilities to be preserved")
	}
}

// TestInspectModelDoesNotTrustBareOK guards against the earlier behaviour where
// any 2xx marked tools/reasoning/vision as supported, because OpenAI-compatible
// servers routinely accept and ignore those parameters.
func TestInspectModelDoesNotTrustBareOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always 200 with plain text, ignoring tools / reasoning_effort / images.
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "OK"}}},
		})
	}))
	defer ts.Close()

	inspected := InspectModel(context.Background(), ts.URL+"/v1", "token", ProviderModel{ID: "plain-model"})
	if inspected.InspectStatus != "ready" {
		t.Fatalf("expected status ready, got %s", inspected.InspectStatus)
	}
	for _, c := range inspected.Capabilities {
		switch c {
		case "tools", "reasoning":
			t.Errorf("capability %q must not be inferred from an empty 200 response", c)
		}
	}
}

// TestInspectErrorRedactsToken ensures the stored inspection error cannot leak
// the provider credential back into the console.
func TestInspectErrorRedactsToken(t *testing.T) {
	const secret = "sk-super-secret-token-value"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key ` + secret + `"}`))
	}))
	defer ts.Close()

	inspected := InspectModel(context.Background(), ts.URL+"/v1", secret, ProviderModel{ID: "m"})
	if inspected.InspectStatus != "auth_error" {
		t.Fatalf("expected auth_error, got %s", inspected.InspectStatus)
	}
	if strings.Contains(inspected.InspectError, secret) {
		t.Errorf("inspection error leaked the provider token: %s", inspected.InspectError)
	}
}

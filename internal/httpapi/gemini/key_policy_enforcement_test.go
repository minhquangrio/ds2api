package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/usageledger"
)

func setupGeminiPolicyTestHandler(t *testing.T, cfgJSON string) (*Handler, *usageledger.Store, *chi.Mux) {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", cfgJSON)
	store := config.LoadStore()
	pool := account.NewPool(store)
	res := auth.NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "token-1", nil
	})
	ledgerPath := filepath.Join(t.TempDir(), "ledger.json")
	ledger := usageledger.New(ledgerPath)

	h := &Handler{
		Store:       store,
		Auth:        res,
		DS:          &testGeminiDS{resp: makeGeminiUpstreamResponse(`data: {"p":"response/content","v":"ok"}`)},
		UsageLedger: ledger,
	}
	r := chi.NewRouter()
	RegisterRoutes(r, h)
	return h, ledger, r
}

func TestGeminiGenerateContentEnforcesModelAllowlist(t *testing.T) {
	cfgJSON := `{
		"api_keys": [
			{"key": "sk-gemini-test", "models": ["gemini-2.5-flash"]}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	_, _, r := setupGeminiPolicyTestHandler(t, cfgJSON)

	// Disallowed model
	reqBody := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/deepseek-v4-pro:generateContent", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-gemini-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "requested model is not allowed") {
		t.Fatalf("expected model not allowed error, got %s", rec.Body.String())
	}
}

func TestGeminiGenerateContentEnforcesQuota(t *testing.T) {
	cfgJSON := `{
		"model_fallback_to_deepseek": true,
		"api_keys": [
			{"key": "sk-gemini-quota", "quota_tokens": 100}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	_, ledger, r := setupGeminiPolicyTestHandler(t, cfgJSON)

	callerID := auth.CallerTokenID("sk-gemini-quota")
	recUsage := usageledger.Begin(ledger, usageledger.Meta{
		CallerID: callerID,
		Model:    "gemini-2.5-flash",
	})
	recUsage.Record(usageledger.Outcome{
		Status: "success",
		Usage: map[string]any{
			"total_tokens": 150,
		},
	})

	reqBody := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-gemini-quota")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "quota exceeded") {
		t.Fatalf("expected quota exceeded error, got %s", rec.Body.String())
	}
}

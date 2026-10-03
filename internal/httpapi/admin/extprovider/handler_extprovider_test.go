package extprovider

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	extpkg "ds2api/internal/extprovider"
)

func TestExtProviderAdminRoutes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DS2API_EXTERNAL_PROVIDERS_PATH", filepath.Join(tmpDir, "providers.json"))

	store := config.LoadStore()
	h := &Handler{Store: store}

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	// 1. Create provider
	body := `{"name":"OpenAI","base_url":"https://api.openai.com/v1","token":"sk-test","model_source":"manual","models":[{"id":"gpt-4o"}]}`
	req := httptest.NewRequest(http.MethodPost, "/providers", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var created extpkg.ProviderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if created.Name != "OpenAI" || !created.HasToken || len(created.Models) != 1 {
		t.Fatalf("unexpected provider response: %+v", created)
	}

	// 2. List providers
	req = httptest.NewRequest(http.MethodGet, "/providers", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 3. Get single provider
	req = httptest.NewRequest(http.MethodGet, "/providers/"+created.ID, nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 4. Update provider
	updateBody := `{"name":"OpenAI Updated"}`
	req = httptest.NewRequest(http.MethodPut, "/providers/"+created.ID, bytes.NewBufferString(updateBody))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 5. Delete provider
	req = httptest.NewRequest(http.MethodDelete, "/providers/"+created.ID, nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

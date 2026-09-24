package usage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	"ds2api/internal/usageledger"
)

func newUsageAdminHarness(t *testing.T) (*Handler, *usageledger.Store) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write config failed: %v", err)
	}
	t.Setenv("DS2API_CONFIG_PATH", configPath)
	t.Setenv("DS2API_ADMIN_KEY", "admin")
	t.Setenv("DS2API_CONFIG_JSON", "")
	store, err := config.LoadStoreWithError()
	if err != nil {
		t.Fatalf("load config store failed: %v", err)
	}
	ledgerPath := filepath.Join(dir, "usage_ledger.json")
	ledger := usageledger.New(ledgerPath)
	return &Handler{Store: store, UsageLedger: ledger}, ledger
}

func TestGetUsageEndpoints(t *testing.T) {
	h, ledger := newUsageAdminHarness(t)
	defer func() {
		_ = ledger.Close()
	}()

	rec := usageledger.Begin(ledger, usageledger.Meta{
		Model:     "deepseek-chat",
		CallerID:  "caller:1",
		AccountID: "user@example.com",
	})
	rec.Record(usageledger.Outcome{
		Status:     "success",
		StatusCode: 200,
		Usage: map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		},
	})

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	// 1. 200 + ETag + body contains all required keys
	req := httptest.NewRequest(http.MethodGet, "/usage?range=all", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("expected non-empty ETag")
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal json failed: %v", err)
	}

	requiredKeys := []string{
		"version", "revision", "flushed_at", "path", "backfill",
		"range", "totals", "models", "accounts", "callers",
		"timeline", "recent",
	}
	for _, key := range requiredKeys {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in response payload", key)
		}
	}

	// 2. If-None-Match -> 304
	req304 := httptest.NewRequest(http.MethodGet, "/usage?range=all", nil)
	req304.Header.Set("If-None-Match", etag)
	w304 := httptest.NewRecorder()
	r.ServeHTTP(w304, req304)

	if w304.Code != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", w304.Code)
	}

	// 3. range=bogus -> 400
	reqBogus := httptest.NewRequest(http.MethodGet, "/usage?range=bogus", nil)
	wBogus := httptest.NewRecorder()
	r.ServeHTTP(wBogus, reqBogus)
	if wBogus.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bogus range, got %d", wBogus.Code)
	}

	// 4. range=custom missing start -> 400
	reqCustomMissing := httptest.NewRequest(http.MethodGet, "/usage?range=custom&end=1000", nil)
	wCustomMissing := httptest.NewRecorder()
	r.ServeHTTP(wCustomMissing, reqCustomMissing)
	if wCustomMissing.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing custom start, got %d", wCustomMissing.Code)
	}

	// 5. Ledger nil -> 503
	hNil := &Handler{Store: h.Store, UsageLedger: nil}
	rNil := chi.NewRouter()
	RegisterRoutes(rNil, hNil)
	reqNil := httptest.NewRequest(http.MethodGet, "/usage", nil)
	wNil := httptest.NewRecorder()
	rNil.ServeHTTP(wNil, reqNil)
	if wNil.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for nil ledger, got %d", wNil.Code)
	}
}

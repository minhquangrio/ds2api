package responses

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

func setupResponsesPolicyTestHandler(t *testing.T, cfgJSON string) (*Handler, *usageledger.Store, *chi.Mux) {
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
		UsageLedger: ledger,
	}
	r := chi.NewRouter()
	RegisterRoutes(r, h)
	return h, ledger, r
}

func TestResponsesEnforcesModelAllowlist(t *testing.T) {
	cfgJSON := `{
		"api_keys": [
			{"key": "sk-resp-test", "models": ["deepseek-v4-flash"]}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	_, _, r := setupResponsesPolicyTestHandler(t, cfgJSON)

	// Disallowed model
	reqBody := `{"model":"deepseek-v4-pro","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-resp-test")
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

func TestResponsesEnforcesQuota(t *testing.T) {
	cfgJSON := `{
		"api_keys": [
			{"key": "sk-resp-quota", "quota_tokens": 100}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	_, ledger, r := setupResponsesPolicyTestHandler(t, cfgJSON)

	callerID := auth.CallerTokenID("sk-resp-quota")
	recUsage := usageledger.Begin(ledger, usageledger.Meta{
		CallerID: callerID,
		Model:    "deepseek-v4-flash",
	})
	recUsage.Record(usageledger.Outcome{
		Status: "success",
		Usage: map[string]any{
			"total_tokens": 150,
		},
	})

	reqBody := `{"model":"deepseek-v4-flash","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-resp-quota")
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

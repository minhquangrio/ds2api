package claude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/usageledger"
)

func setupClaudePolicyTestHandler(t *testing.T, cfgJSON string) (*Handler, *usageledger.Store) {
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
		DS:          &claudeCurrentInputDS{},
		UsageLedger: ledger,
	}
	return h, ledger
}

func TestClaudeMessagesEnforcesModelAllowlist(t *testing.T) {
	cfgJSON := `{
		"api_keys": [
			{"key": "sk-claude-test", "models": ["deepseek-v4-flash"]}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	h, _ := setupClaudePolicyTestHandler(t, cfgJSON)

	// Model resolves to deepseek-v4-pro (disallowed)
	reqBody := `{"model":"deepseek-v4-pro","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("x-api-key", "sk-claude-test")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Messages(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "requested model is not allowed") {
		t.Fatalf("expected model not allowed error, got %s", rec.Body.String())
	}
}

func TestClaudeMessagesEnforcesQuota(t *testing.T) {
	cfgJSON := `{
		"api_keys": [
			{"key": "sk-claude-quota", "quota_tokens": 100}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	h, ledger := setupClaudePolicyTestHandler(t, cfgJSON)

	callerID := auth.CallerTokenID("sk-claude-quota")
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

	reqBody := `{"model":"deepseek-v4-flash","max_tokens":1024,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("x-api-key", "sk-claude-quota")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.Messages(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "quota exceeded") {
		t.Fatalf("expected quota exceeded error, got %s", rec.Body.String())
	}
}

package chat

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

func setupPolicyTestHandler(t *testing.T, cfgJSON string) (*Handler, *config.Store, *usageledger.Store) {
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
		DS:          streamStatusDSStub{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}},
		UsageLedger: ledger,
	}
	return h, store, ledger
}

func TestChatCompletionsEnforcesModelAllowlist(t *testing.T) {
	cfgJSON := `{
		"api_keys": [
			{"key": "sk-limited", "models": ["deepseek-v4-flash"]}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	h, _, _ := setupPolicyTestHandler(t, cfgJSON)

	// 1. Model forbidden
	reqBody := `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-limited")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "requested model is not allowed") {
		t.Fatalf("expected error message about model not allowed, got %s", rec.Body.String())
	}

	// 2. Allowed model (resolves to deepseek-v4-flash)
	reqBody2 := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody2))
	req2.Header.Set("Authorization", "Bearer sk-limited")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()

	h.ChatCompletions(rec2, req2)
	// It should NOT be 403 forbidden
	if rec2.Code == http.StatusForbidden {
		t.Fatalf("expected allowed model not to return 403, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestChatCompletionsEnforcesTokenQuota(t *testing.T) {
	cfgJSON := `{
		"api_keys": [
			{"key": "sk-quota-test", "quota_tokens": 500}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	h, _, ledger := setupPolicyTestHandler(t, cfgJSON)

	// Record usage that reaches the quota
	callerID := auth.CallerTokenID("sk-quota-test")
	recUsage := usageledger.Begin(ledger, usageledger.Meta{
		CallerID: callerID,
		Model:    "deepseek-v4-flash",
	})
	recUsage.Record(usageledger.Outcome{
		Status: "success",
		Usage: map[string]any{
			"prompt_tokens":     300,
			"completion_tokens": 200,
			"total_tokens":      500,
		},
	})

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-quota-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "quota exceeded") {
		t.Fatalf("expected quota exceeded error, got %s", rec.Body.String())
	}
}

func TestVercelStreamPrepareEnforcesModelAndQuota(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	cfgJSON := `{
		"api_keys": [
			{"key": "sk-vercel-test", "models": ["deepseek-v4-flash"], "quota_tokens": 1000}
		],
		"accounts": [
			{"email": "test@example.com", "password": "pwd", "token": "tok1"}
		]
	}`
	h, _, ledger := setupPolicyTestHandler(t, cfgJSON)

	// 1. Model forbidden in Vercel prepare
	reqBody := `{"model":"deepseek-v4-pro","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer sk-vercel-test")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for vercel prepare with disallowed model, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if len(h.streamLeases) != 0 {
		t.Fatalf("expected no lease created on forbidden model, got %d", len(h.streamLeases))
	}

	// 2. Quota exceeded in Vercel prepare
	callerID := auth.CallerTokenID("sk-vercel-test")
	recUsage := usageledger.Begin(ledger, usageledger.Meta{
		CallerID: callerID,
		Model:    "deepseek-v4-flash",
	})
	recUsage.Record(usageledger.Outcome{
		Status: "success",
		Usage: map[string]any{
			"prompt_tokens":     600,
			"completion_tokens": 500,
			"total_tokens":      1100,
		},
	})

	reqBody2 := `{"model":"deepseek-v4-flash","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(reqBody2))
	req2.Header.Set("Authorization", "Bearer sk-vercel-test")
	req2.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests for vercel prepare when quota exceeded, got %d, body=%s", rec2.Code, rec2.Body.String())
	}
	if len(h.streamLeases) != 0 {
		t.Fatalf("expected no lease created on quota exceeded, got %d", len(h.streamLeases))
	}
}

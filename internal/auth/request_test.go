package auth

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"ds2api/internal/account"
	"ds2api/internal/config"
)

func newTestResolver(t *testing.T) *Resolver {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[{"email":"acc@example.com","password":"pwd","token":"account-token"}]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	return NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "fresh-token", nil
	})
}

func TestDetermineWithXAPIKeyUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	req.Header.Set("x-api-key", "direct-token")

	auth, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if auth.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if auth.DeepSeekToken != "direct-token" {
		t.Fatalf("unexpected token: %q", auth.DeepSeekToken)
	}
	if auth.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
}

func TestDetermineWithXAPIKeyManagedKeyAcquiresAccount(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/anthropic/v1/messages", nil)
	req.Header.Set("x-api-key", "managed-key")

	auth, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(auth)
	if !auth.UseConfigToken {
		t.Fatalf("expected managed key mode")
	}
	if auth.AccountID != "acc@example.com" {
		t.Fatalf("unexpected account id: %q", auth.AccountID)
	}
	if auth.DeepSeekToken != "fresh-token" {
		t.Fatalf("unexpected account token: %q", auth.DeepSeekToken)
	}
	if auth.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
}

func TestDetermineCallerWithManagedKeySkipsAccountAcquire(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodGet, "/v1/responses/resp_1", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := r.DetermineCaller(req)
	if err != nil {
		t.Fatalf("determine caller failed: %v", err)
	}
	if a.CallerID == "" {
		t.Fatalf("expected caller id to be populated")
	}
	if a.UseConfigToken {
		t.Fatalf("expected no config-token lease for caller-only auth")
	}
	if a.AccountID != "" {
		t.Fatalf("expected empty account id, got %q", a.AccountID)
	}
}

func TestCallerTokenIDStable(t *testing.T) {
	a := callerTokenID("token-a")
	b := callerTokenID("token-a")
	c := callerTokenID("token-b")
	if a == "" || b == "" || c == "" {
		t.Fatalf("expected non-empty caller ids")
	}
	if a != b {
		t.Fatalf("expected stable caller id, got %q and %q", a, b)
	}
	if a == c {
		t.Fatalf("expected different caller id for different tokens")
	}
}

func TestDetermineMissingToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	_, err := r.Determine(req)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if err != ErrUnauthorized {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetermineWithQueryKeyUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=direct-query-key", nil)

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if a.DeepSeekToken != "direct-query-key" {
		t.Fatalf("unexpected token: %q", a.DeepSeekToken)
	}
}

func TestDetermineWithXGoogAPIKeyUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse", nil)
	req.Header.Set("x-goog-api-key", "goog-header-key")

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if a.DeepSeekToken != "goog-header-key" {
		t.Fatalf("unexpected token: %q", a.DeepSeekToken)
	}
}

func TestDetermineWithAPIKeyQueryParamUsesDirectToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?api_key=direct-api-key", nil)

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a.UseConfigToken {
		t.Fatalf("expected direct token mode")
	}
	if a.DeepSeekToken != "direct-api-key" {
		t.Fatalf("unexpected token: %q", a.DeepSeekToken)
	}
}

func TestDetermineHeaderTokenPrecedenceOverQueryKey(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent?key=query-key", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := r.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer r.Release(a)
	if !a.UseConfigToken {
		t.Fatalf("expected managed key mode from header token")
	}
	if a.AccountID == "" {
		t.Fatalf("expected managed account to be acquired")
	}
}

func TestDetermineCallerMissingToken(t *testing.T) {
	r := newTestResolver(t)
	req, _ := http.NewRequest(http.MethodGet, "/v1/responses/resp_1", nil)

	_, err := r.DetermineCaller(req)
	if err == nil {
		t.Fatal("expected unauthorized error")
	}
	if err != ErrUnauthorized {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDetermineManagedAccountForcesRefreshEverySixHours(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[{"email":"acc@example.com","password":"pwd","token":"seed-token"}]
	}`)
	store := config.LoadStore()
	if err := store.UpdateAccountToken("acc@example.com", "seed-token"); err != nil {
		t.Fatalf("update token failed: %v", err)
	}
	pool := account.NewPool(store)

	var loginCount int32
	resolver := NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		n := atomic.AddInt32(&loginCount, 1)
		return "fresh-token-" + string(rune('0'+n)), nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	a1, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a1.DeepSeekToken != "seed-token" {
		t.Fatalf("expected initial token without forced refresh, got %q", a1.DeepSeekToken)
	}
	resolver.Release(a1)
	if got := atomic.LoadInt32(&loginCount); got != 0 {
		t.Fatalf("expected no login before refresh interval, got %d", got)
	}

	resolver.mu.Lock()
	resolver.tokenRefreshedAt["acc@example.com"] = time.Now().Add(-7 * time.Hour)
	resolver.mu.Unlock()

	a2, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine after interval failed: %v", err)
	}
	defer resolver.Release(a2)
	if a2.DeepSeekToken != "fresh-token-1" {
		t.Fatalf("expected refreshed token after interval, got %q", a2.DeepSeekToken)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected exactly one forced refresh login, got %d", got)
	}
}

func TestDetermineManagedAccountUsesUpdatedRefreshInterval(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[{"email":"acc@example.com","password":"pwd","token":"seed-token"}],
		"runtime":{"token_refresh_interval_hours":6}
	}`)
	store := config.LoadStore()
	if err := store.UpdateAccountToken("acc@example.com", "seed-token"); err != nil {
		t.Fatalf("update token failed: %v", err)
	}
	pool := account.NewPool(store)

	var loginCount int32
	resolver := NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		n := atomic.AddInt32(&loginCount, 1)
		return "fresh-token-" + string(rune('0'+n)), nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	a1, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	if a1.DeepSeekToken != "seed-token" {
		t.Fatalf("expected initial token without forced refresh, got %q", a1.DeepSeekToken)
	}
	resolver.Release(a1)
	if got := atomic.LoadInt32(&loginCount); got != 0 {
		t.Fatalf("expected no login before runtime update, got %d", got)
	}

	if err := store.Update(func(c *config.Config) error {
		c.Runtime.TokenRefreshIntervalHours = 1
		return nil
	}); err != nil {
		t.Fatalf("update runtime failed: %v", err)
	}

	resolver.mu.Lock()
	resolver.tokenRefreshedAt["acc@example.com"] = time.Now().Add(-2 * time.Hour)
	resolver.mu.Unlock()

	a2, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine after runtime update failed: %v", err)
	}
	defer resolver.Release(a2)
	if a2.DeepSeekToken != "fresh-token-1" {
		t.Fatalf("expected refreshed token after runtime update, got %q", a2.DeepSeekToken)
	}
	if got := atomic.LoadInt32(&loginCount); got != 1 {
		t.Fatalf("expected exactly one login after runtime update, got %d", got)
	}
}

func TestDetermineManagedAccountRetriesOtherAccountOnLoginFailure(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"bad@example.com","password":"pwd"},
			{"email":"good@example.com","password":"pwd","token":"good-token"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, func(_ context.Context, acc config.Account) (string, error) {
		if acc.Email == "bad@example.com" {
			return "", errors.New("stale account")
		}
		return "fresh-good-token", nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	a, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)
	if a.AccountID != "good@example.com" {
		t.Fatalf("expected fallback to good account, got %q", a.AccountID)
	}
	if a.DeepSeekToken == "" {
		t.Fatal("expected non-empty token from fallback account")
	}
	if !a.TriedAccounts["bad@example.com"] {
		t.Fatalf("expected bad account to be tracked as tried")
	}
}

func TestDetermineTargetAccountDoesNotFallbackOnLoginFailure(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"bad@example.com","password":"pwd"},
			{"email":"good@example.com","password":"pwd","token":"good-token"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, func(_ context.Context, acc config.Account) (string, error) {
		if acc.Email == "bad@example.com" {
			return "", errors.New("stale account")
		}
		return "fresh-good-token", nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	req.Header.Set("X-Ds2-Target-Account", "bad@example.com")

	_, err := resolver.Determine(req)
	if err == nil {
		t.Fatal("expected determine to fail for broken target account")
	}
}

func TestDetermineManagedAccountReturnsLastEnsureErrorWhenAllFail(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"bad1@example.com","password":"pwd"},
			{"email":"bad2@example.com","password":"pwd"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	ensureErr := errors.New("all credentials stale")
	resolver := NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "", ensureErr
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	_, err := resolver.Determine(req)
	if err == nil {
		t.Fatal("expected determine to fail")
	}
	if !errors.Is(err, ensureErr) {
		t.Fatalf("expected ensure error, got %v", err)
	}
	if errors.Is(err, ErrNoAccount) {
		t.Fatalf("expected auth-style ensure error, got ErrNoAccount")
	}
}

func TestMapAuthStatus(t *testing.T) {
	status, msg := MapAuthStatus(ErrUnauthorized)
	if status != http.StatusUnauthorized || msg != "Unauthorized" {
		t.Fatalf("expected 401 Unauthorized, got %d %q", status, msg)
	}

	status, msg = MapAuthStatus(ErrNoProviderAccount)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d %q", status, msg)
	}

	status, msg = MapAuthStatus(ErrNoAccount)
	if status != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d %q", status, msg)
	}

	status, msg = MapAuthStatus(ErrAllAccountsBusy)
	if status != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d %q", status, msg)
	}
}

func TestDetermineForProviderGeminiNoAccountNoFallback(t *testing.T) {
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"api_keys": [{"key": "managed-key"}],
		"accounts": [{"email": "ds@example.com", "token": "ds-token", "provider": "deepseek"}],
		"model_fallback_to_deepseek": false
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, nil)

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	_, err := resolver.DetermineForProvider(req, "gemini")
	if !errors.Is(err, ErrNoProviderAccount) {
		t.Fatalf("expected ErrNoProviderAccount, got %v", err)
	}
}

func TestDetermineForProviderGeminiFallbackToDeepSeek(t *testing.T) {
	t.Setenv("DS2API_CONFIG_PATH", t.TempDir()+"/config.json")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"api_keys": [{"key": "managed-key"}],
		"accounts": [{"email": "ds@example.com", "token": "ds-token", "provider": "deepseek"}],
		"model_fallback_to_deepseek": true
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "mock-token", nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")

	authCtx, err := resolver.DetermineForProvider(req, "gemini")
	if err != nil {
		t.Fatalf("expected fallback to deepseek to succeed, got %v", err)
	}
	if authCtx.Provider != "deepseek" {
		t.Fatalf("expected provider deepseek after fallback, got %s", authCtx.Provider)
	}
	if authCtx.AccountID != "ds@example.com" {
		t.Fatalf("expected account ds@example.com, got %s", authCtx.AccountID)
	}
}

func TestDetermineRespectsAccountAllowlist(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"api_keys": [
			{"key": "key-assigned", "accounts": ["acc2@example.com"]},
			{"key": "key-all"}
		],
		"accounts": [
			{"email": "acc1@example.com", "token": "token-1"},
			{"email": "acc2@example.com", "token": "token-2"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "token", nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer key-assigned")

	authCtx, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("unexpected determine error: %v", err)
	}
	defer resolver.Release(authCtx)
	if authCtx.AccountID != "acc2@example.com" {
		t.Errorf("expected acc2@example.com, got %s", authCtx.AccountID)
	}
}

func TestDetermineTargetAccountConflict(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"api_keys": [
			{"key": "key-assigned", "accounts": ["acc1@example.com"]}
		],
		"accounts": [
			{"email": "acc1@example.com", "token": "token-1"},
			{"email": "acc2@example.com", "token": "token-2"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, nil)

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer key-assigned")
	req.Header.Set("X-Ds2-Target-Account", "acc2@example.com")

	_, err := resolver.Determine(req)
	if !errors.Is(err, ErrAccountNotAllowed) {
		t.Fatalf("expected ErrAccountNotAllowed, got %v", err)
	}
	code, msg := MapAuthStatus(err)
	if code != http.StatusForbidden {
		t.Errorf("expected 403, got %d (msg: %s)", code, msg)
	}
}

func TestDetermineAllowlistExhaustionNoInfiniteLoop(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"api_keys": [
			{"key": "key-assigned", "accounts": ["acc1@example.com"]}
		],
		"accounts": [
			{"email": "acc1@example.com", "token": "token-1"},
			{"email": "acc2@example.com", "token": "token-2"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	// Refresh fails for acc1
	resolver := NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "", errors.New("refresh failed")
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer key-assigned")

	done := make(chan error, 1)
	go func() {
		_, err := resolver.Determine(req)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected error on failed refresh, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Determine hung in infinite loop when allowlist exhausted")
	}
}

func TestSwitchAccountRespectsAllowlist(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"api_keys": [
			{"key": "key-assigned", "accounts": ["acc1@example.com", "acc2@example.com"]}
		],
		"accounts": [
			{"email": "acc1@example.com", "token": "token-1"},
			{"email": "acc2@example.com", "token": "token-2"},
			{"email": "acc3@example.com", "token": "token-3"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, func(_ context.Context, _ config.Account) (string, error) {
		return "valid-token", nil
	})

	req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer key-assigned")

	authCtx, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(authCtx)

	// First account should be acc1 or acc2
	firstID := authCtx.AccountID
	if firstID != "acc1@example.com" && firstID != "acc2@example.com" {
		t.Fatalf("unexpected first account: %s", firstID)
	}

	// Switch account
	switched := resolver.SwitchAccount(context.Background(), authCtx)
	if !switched {
		t.Fatalf("expected SwitchAccount to succeed")
	}
	secondID := authCtx.AccountID
	if secondID != "acc1@example.com" && secondID != "acc2@example.com" {
		t.Fatalf("SwitchAccount acquired unassigned account: %s", secondID)
	}
	if secondID == firstID {
		t.Fatalf("SwitchAccount returned the same account: %s", secondID)
	}

	// Third switch should fail because allowlist (size 2) is exhausted
	switchedAgain := resolver.SwitchAccount(context.Background(), authCtx)
	if switchedAgain {
		t.Fatalf("expected SwitchAccount to fail after allowlist exhausted, but acquired: %s", authCtx.AccountID)
	}
}

type stubCallerTokenReader struct {
	tokens map[string]int64
}

func (s *stubCallerTokenReader) CallerTotalTokens(callerID string) int64 {
	if s == nil || s.tokens == nil {
		return 0
	}
	return s.tokens[callerID]
}

func TestEnforceKeyModelQuota(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"api_keys": [
			{
				"key": "key-restricted",
				"models": ["deepseek-v4-flash"],
				"quota_tokens": 1000
			},
			{
				"key": "key-unlimited",
				"quota_tokens": 0
			}
		],
		"accounts": [
			{"email": "acc@test.com", "token": "tok"}
		]
	}`)
	store := config.LoadStore()
	pool := account.NewPool(store)
	resolver := NewResolver(store, pool, nil)

	callerID := callerTokenID("key-restricted")
	ledger := &stubCallerTokenReader{
		tokens: map[string]int64{
			callerID: 500,
		},
	}

	// 1. Allowed model, quota not exceeded
	{
		req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer key-restricted")
		err := resolver.EnforceKeyModelQuota(req, ledger, "deepseek-v4-flash")
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	}

	// 2. Blocked model -> ErrModelNotAllowed (403)
	{
		req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer key-restricted")
		err := resolver.EnforceKeyModelQuota(req, ledger, "deepseek-v4-pro")
		if !errors.Is(err, ErrModelNotAllowed) {
			t.Errorf("expected ErrModelNotAllowed, got %v", err)
		}
		status, _ := MapAuthStatus(err)
		if status != http.StatusForbidden {
			t.Errorf("expected 403, got %d", status)
		}
	}

	// 3. Quota exceeded -> ErrQuotaExceeded (429)
	{
		ledger.tokens[callerID] = 1000
		req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer key-restricted")
		err := resolver.EnforceKeyModelQuota(req, ledger, "deepseek-v4-flash")
		if !errors.Is(err, ErrQuotaExceeded) {
			t.Errorf("expected ErrQuotaExceeded, got %v", err)
		}
		status, msg := MapAuthStatus(err)
		if status != http.StatusTooManyRequests {
			t.Errorf("expected 429, got %d (msg: %s)", status, msg)
		}
	}

	// 4. Quota 0 (unlimited)
	{
		req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer key-unlimited")
		err := resolver.EnforceKeyModelQuota(req, ledger, "any-model")
		if err != nil {
			t.Errorf("expected nil for unlimited key, got %v", err)
		}
	}

	// 5. Unknown key / direct token -> nil
	{
		req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer sk-direct-token-unknown")
		err := resolver.EnforceKeyModelQuota(req, ledger, "any-model")
		if err != nil {
			t.Errorf("expected nil for direct token, got %v", err)
		}
	}

	// 6. Nil ledger -> nil (graceful)
	{
		req, _ := http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer key-restricted")
		err := resolver.EnforceKeyModelQuota(req, nil, "deepseek-v4-flash")
		if err != nil {
			t.Errorf("expected nil when ledger is nil, got %v", err)
		}
	}
}

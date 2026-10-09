package geminiweb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

type mockStore struct {
	cfg config.Config
}

func (m *mockStore) Snapshot() config.Config {
	return m.cfg
}

func (m *mockStore) FindAccount(id string) (config.Account, bool) {
	for _, acc := range m.cfg.Accounts {
		if acc.Identifier() == id {
			return acc, true
		}
	}
	return config.Account{}, false
}

func (m *mockStore) Update(fn func(*config.Config) error) error {
	return fn(&m.cfg)
}

func TestGeminiCooldownDuration(t *testing.T) {
	t.Run("Default60s", func(t *testing.T) {
		t.Setenv("DS2API_GEMINI_COOLDOWN_SECONDS", "")
		if d := geminiCooldownDuration(); d != 60*time.Second {
			t.Errorf("expected 60s, got %v", d)
		}
	})

	t.Run("CustomDuration", func(t *testing.T) {
		t.Setenv("DS2API_GEMINI_COOLDOWN_SECONDS", "15")
		if d := geminiCooldownDuration(); d != 15*time.Second {
			t.Errorf("expected 15s, got %v", d)
		}
	})

	t.Run("InvalidDurationFallsBackTo60s", func(t *testing.T) {
		t.Setenv("DS2API_GEMINI_COOLDOWN_SECONDS", "invalid")
		if d := geminiCooldownDuration(); d != 60*time.Second {
			t.Errorf("expected 60s fallback, got %v", d)
		}
	})
}

func TestGeminiRetryCeiling(t *testing.T) {
	t.Run("NonSnapshotterStoreReturns3", func(t *testing.T) {
		if c := geminiRetryCeiling(nil); c != 3 {
			t.Errorf("expected 3 for nil store, got %d", c)
		}
	})

	t.Run("DynamicCalculationAndCap", func(t *testing.T) {
		store := &mockStore{
			cfg: config.Config{
				Accounts: []config.Account{
					{Email: "g1@test.com", Provider: "gemini"},
					{Email: "g2@test.com", Provider: "gemini"},
					{Email: "ds1@test.com", Provider: "deepseek"},
				},
			},
		}
		if c := geminiRetryCeiling(store); c != 2 {
			t.Errorf("expected 2 schedulable gemini accounts, got %d", c)
		}

		// Add 5 more gemini accounts -> total 7 -> ceiling caps at 5
		for i := 3; i <= 7; i++ {
			store.cfg.Accounts = append(store.cfg.Accounts, config.Account{
				Email: "g@test.com", Provider: "gemini",
			})
		}
		if c := geminiRetryCeiling(store); c != 5 {
			t.Errorf("expected ceiling of 5, got %d", c)
		}
	})
}

func dummyClientForCookies(rawCookies string) *Client {
	cookiesMap, _, _ := ParseCookies(rawCookies)
	return &Client{
		cookiesMap: cookiesMap,
	}
}

func setupTestAuth(t *testing.T) (*mockStore, *auth.Resolver, *auth.RequestAuth) {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"g1@test.com","provider":"gemini","cookies":"__Secure-1PSID=sid1","token":"tok-1"},
			{"email":"g2@test.com","provider":"gemini","cookies":"__Secure-1PSID=sid2","token":"tok-2"}
		]
	}`)
	store := &mockStore{
		cfg: config.Config{
			Keys: []string{"managed-key"},
			Accounts: []config.Account{
				{Email: "g1@test.com", Provider: "gemini", Cookies: "__Secure-1PSID=sid1", Token: "tok-1"},
				{Email: "g2@test.com", Provider: "gemini", Cookies: "__Secure-1PSID=sid2", Token: "tok-2"},
			},
		},
	}
	realStore := config.LoadStore()
	pool := account.NewPool(realStore)
	resolver := auth.NewResolver(realStore, pool, func(_ context.Context, acc config.Account) (string, error) {
		return acc.Token, nil
	})

	rt := DefaultRuntime()
	rt.mu.Lock()
	rt.clients["g1@test.com"] = dummyClientForCookies("__Secure-1PSID=sid1")
	rt.clients["g2@test.com"] = dummyClientForCookies("__Secure-1PSID=sid2")
	rt.mu.Unlock()

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("x-api-key", "managed-key")
	authCtx, err := resolver.DetermineForProvider(req, "gemini")
	if err != nil {
		t.Fatalf("setup determine failed: %v", err)
	}
	return store, resolver, authCtx
}

func TestExecuteStreamWithRetry_SuccessOnRetry(t *testing.T) {
	_, resolver, authCtx := setupTestAuth(t)
	defer resolver.Release(authCtx)

	callCount := 0
	firstAccountID := authCtx.AccountID
	var switchedTo string

	mockRenderer := func(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
		callCount++
		if callCount == 1 {
			return "", "", ErrGeminiThrottled
		}
		return "thought", "result", nil
	}

	w := httptest.NewRecorder()
	stdReq := promptcompat.StandardRequest{ResponseModel: "gemini-2.5-flash"}

	thinking, text, err := ExecuteStreamWithRetry(
		context.Background(), authCtx, resolver.Store, stdReq, w, mockRenderer,
		func(newID string) {
			switchedTo = newID
		},
	)

	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
	if thinking != "thought" || text != "result" {
		t.Errorf("unexpected output: %s / %s", thinking, text)
	}
	if switchedTo == "" || switchedTo == firstAccountID {
		t.Errorf("expected switch to different account, got %s (first: %s)", switchedTo, firstAccountID)
	}
}

func TestExecuteStreamWithRetry_AllAccountsCoolingDown(t *testing.T) {
	_, resolver, authCtx := setupTestAuth(t)
	defer resolver.Release(authCtx)

	mockRenderer := func(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
		return "", "", ErrGeminiThrottled
	}

	w := httptest.NewRecorder()
	stdReq := promptcompat.StandardRequest{ResponseModel: "gemini-2.5-flash"}

	_, _, err := ExecuteStreamWithRetry(
		context.Background(), authCtx, resolver.Store, stdReq, w, mockRenderer, nil,
	)

	if !errors.Is(err, ErrAllGeminiAccountsCoolingDown) {
		t.Fatalf("expected ErrAllGeminiAccountsCoolingDown, got: %v", err)
	}
}

func TestExecuteStreamWithRetry_MidStreamThrottleDoesNotRetry(t *testing.T) {
	_, resolver, authCtx := setupTestAuth(t)
	defer resolver.Release(authCtx)

	callCount := 0
	midStreamErr := errors.New("gemini upstream throttled mid-stream (BardErrorInfo 1095); output truncated")

	mockRenderer := func(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
		callCount++
		return "partial-thought", "partial-text", midStreamErr
	}

	w := httptest.NewRecorder()
	stdReq := promptcompat.StandardRequest{ResponseModel: "gemini-2.5-flash"}

	thinking, text, err := ExecuteStreamWithRetry(
		context.Background(), authCtx, resolver.Store, stdReq, w, mockRenderer, nil,
	)

	if !errors.Is(err, midStreamErr) {
		t.Fatalf("expected midStreamErr to be returned directly, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("mid-stream failure must NOT retry, callCount=%d", callCount)
	}
	if thinking != "partial-thought" || text != "partial-text" {
		t.Errorf("expected partial output returned, got: %s / %s", thinking, text)
	}
}

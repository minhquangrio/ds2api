package geminiweb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ds2api/internal/config"
)

type mockRefresherStore struct {
	accounts []config.Account
}

func (m *mockRefresherStore) Snapshot() config.Config {
	return config.Config{
		Accounts: m.accounts,
	}
}

func TestRotateAndInitSessionCooldownAndSingleFlight(t *testing.T) {
	var rotateCount int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "RotateCookies") {
			atomic.AddInt64(&rotateCount, 1)
			w.Header().Add("Set-Cookie", "__Secure-1PSIDTS=new_token_123; Path=/; Domain=.google.com")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
			return
		}
		if strings.Contains(r.URL.Path, "app") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`"SNlM0e":"fake_at","cfb2h":"fake_bl","FdrFJe":"fake_sid","TuX5cc":"en","qKIAYe":"fake_push"`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	rawCookies := "__Secure-1PSID=test_psid; __Secure-1PSIDTS=test_ts"
	client, err := NewClient(rawCookies, ClientOptions{
		RotateURL:          server.URL + "/RotateCookies",
		AppURL:             server.URL + "/app",
		InsecureSkipVerify: true,
		ForceHTTP1:         true,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// First rotation should succeed
	err = client.RotateAndInitSession(ctx, RotateSessionOptions{Force: false, SkipDiscovery: true})
	if err != nil {
		t.Fatalf("first rotate failed: %v", err)
	}

	// Immediate second rotation without Force must be throttled (60s cooldown)
	err = client.RotateAndInitSession(ctx, RotateSessionOptions{Force: false, SkipDiscovery: true})
	if !errors.Is(err, ErrRotationThrottled) {
		t.Fatalf("expected ErrRotationThrottled, got %v", err)
	}

	// Second rotation with Force within 10s must still be throttled by the 10s floor
	err = client.RotateAndInitSession(ctx, RotateSessionOptions{Force: true, SkipDiscovery: true})
	if !errors.Is(err, ErrRotationThrottled) {
		t.Fatalf("expected ErrRotationThrottled on Force within 10s floor, got %v", err)
	}

	// Verify single-flight under concurrency
	var wg sync.WaitGroup
	var throttledCount int64
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rErr := client.RotateAndInitSession(ctx, RotateSessionOptions{Force: false, SkipDiscovery: true}); errors.Is(rErr, ErrRotationThrottled) {
				atomic.AddInt64(&throttledCount, 1)
			}
		}()
	}
	wg.Wait()

	if throttledCount != 5 {
		t.Fatalf("expected all 5 concurrent calls to be throttled, got %d", throttledCount)
	}
}

func TestBatchCookieUpdateTriggersSingleCallback(t *testing.T) {
	rawCookies := "__Secure-1PSID=test_psid; __Secure-1PSIDTS=old_ts"
	client, err := NewClient(rawCookies)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	var cbCount int64
	client.SetOnCookieUpdate(func(newCookies string) {
		atomic.AddInt64(&cbCount, 1)
	})

	headers := map[string][]string{
		"Set-Cookie": {
			"__Secure-1PSIDTS=updated_ts; Path=/",
			"SIDCC=sidcc_val; Path=/",
			"__Secure-3PSIDTS=updated_3pts; Path=/",
		},
	}

	newTS := client.checkSetCookies(headers)
	if newTS != "updated_ts" {
		t.Errorf("expected updated_ts, got %s", newTS)
	}
	if atomic.LoadInt64(&cbCount) != 1 {
		t.Errorf("expected exactly 1 callback for batch update, got %d", cbCount)
	}
}

func TestRetryOn401StreamGenerate(t *testing.T) {
	var streamCalls int64
	var rotateCalls int64

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "StreamGenerate") {
			call := atomic.AddInt64(&streamCalls, 1)
			if call == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Unauthorized"))
				return
			}
			w.WriteHeader(http.StatusOK)
			payload := `[["wrb.fr",null,"[null,[\"cid_123\",\"rid_456\"],null,null,[[\"rcid_789\",[\"retry_success\"],null,null,null,null,null,null,[2]]]]"]]`
			_, _ = w.Write([]byte(buildFrame(payload, true)))
			return
		}
		if strings.Contains(r.URL.Path, "RotateCookies") {
			atomic.AddInt64(&rotateCalls, 1)
			w.Header().Add("Set-Cookie", "__Secure-1PSIDTS=recovered_ts; Path=/")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if strings.Contains(r.URL.Path, "app") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`"SNlM0e":"new_at","cfb2h":"new_bl","FdrFJe":"new_sid","TuX5cc":"en","qKIAYe":"new_push"`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient("__Secure-1PSID=test_psid; __Secure-1PSIDTS=expired_ts", ClientOptions{
		RotateURL:          server.URL + "/RotateCookies",
		AppURL:             server.URL + "/app",
		StreamGenerateURL:  server.URL + "/StreamGenerate",
		InsecureSkipVerify: true,
		ForceHTTP1:         true,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	client.SetSession(SessionParams{
		AccessToken: "old_at",
		BuildLabel:  "old_bl",
		Language:    "en",
	})

	ctx := context.Background()
	res, err := client.GenerateWithRetry(ctx, "hello", GenerateOptions{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatalf("GenerateWithRetry failed: %v", err)
	}

	if atomic.LoadInt64(&streamCalls) != 2 {
		t.Errorf("expected 2 stream calls (1 fail + 1 retry), got %d", streamCalls)
	}
	if atomic.LoadInt64(&rotateCalls) != 1 {
		t.Errorf("expected 1 rotate call during retry, got %d", rotateCalls)
	}
	if !strings.Contains(res.Text, "retry_success") {
		t.Errorf("expected retry_success in result text, got %s", res.Text)
	}
}

func TestWorkerLifecycle(t *testing.T) {
	rt := &Runtime{
		clients: make(map[string]*Client),
	}

	mockStore := &mockRefresherStore{
		accounts: []config.Account{
			{Name: "test_acc", Provider: "gemini", Cookies: "__Secure-1PSID=a"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt.StartBackgroundRefresher(ctx, mockStore, 60*time.Second)
	// Stop immediately to ensure clean exit without deadlock or leak
	rt.StopBackgroundRefresher()
}

func TestRetryFailFastOnRotateError(t *testing.T) {
	var streamCalls int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "StreamGenerate") {
			atomic.AddInt64(&streamCalls, 1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized"))
			return
		}
		if strings.Contains(r.URL.Path, "RotateCookies") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Cookie Revoked"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient("__Secure-1PSID=test_psid; __Secure-1PSIDTS=expired_ts", ClientOptions{
		RotateURL:          server.URL + "/RotateCookies",
		StreamGenerateURL:  server.URL + "/StreamGenerate",
		InsecureSkipVerify: true,
		ForceHTTP1:         true,
	})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	client.SetSession(SessionParams{
		AccessToken: "old_at",
		BuildLabel:  "old_bl",
		Language:    "en",
	})

	ctx := context.Background()
	_, err = client.GenerateWithRetry(ctx, "hello", GenerateOptions{Model: "gemini-2.5-flash"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("expected ErrUnauthenticated on fail-fast, got %v", err)
	}
	if atomic.LoadInt64(&streamCalls) != 1 {
		t.Errorf("expected exactly 1 stream call (fail-fast without retry), got %d", streamCalls)
	}
}

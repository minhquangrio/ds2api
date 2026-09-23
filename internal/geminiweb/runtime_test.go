package geminiweb

import (
	"context"
	"testing"

	"ds2api/internal/config"
)

type dummyStore struct {
	updatedCookies map[string]string
}

func (d *dummyStore) UpdateAccountCookies(id, cookies string) error {
	if d.updatedCookies == nil {
		d.updatedCookies = make(map[string]string)
	}
	d.updatedCookies[id] = cookies
	return nil
}

func (d *dummyStore) Snapshot() config.Config {
	return config.Config{
		Proxies: []config.Proxy{
			{
				ID:   "p1",
				Type: "socks5",
				Host: "127.0.0.1",
				Port: 1080,
			},
		},
	}
}

func TestFormatProxyURL(t *testing.T) {
	p := config.Proxy{
		Type:     "http",
		Host:     "proxy.local",
		Port:     8080,
		Username: "user",
		Password: "password",
	}
	u := FormatProxyURL(p)
	expected := "http://user:password@proxy.local:8080"
	if u != expected {
		t.Fatalf("expected %s, got %s", expected, u)
	}

	p2 := config.Proxy{
		Type: "socks5h",
		Host: "127.0.0.1",
		Port: 9050,
	}
	u2 := FormatProxyURL(p2)
	expected2 := "socks5://127.0.0.1:9050"
	if u2 != expected2 {
		t.Fatalf("expected %s, got %s", expected2, u2)
	}
}

func TestRuntimeGetClientValidation(t *testing.T) {
	rt := DefaultRuntime()
	store := &dummyStore{}
	_ = store.UpdateAccountCookies("test", "cookie")
	_ = store.Snapshot()
	acc := config.Account{
		Email:    "test@example.com",
		Provider: "gemini",
		Cookies:  "",
	}
	_, err := rt.GetClient(context.Background(), acc, store)
	if err == nil {
		t.Fatalf("expected error for empty cookies, got nil")
	}
}

func TestClientMatches(t *testing.T) {
	c, err := NewClient("__Secure-1PSID=sid1; __Secure-1PSIDTS=ts1", ClientOptions{
		Proxy: "socks5://127.0.0.1:1080",
	})
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	defer func() {
		if closeErr := c.Close(); closeErr != nil {
			t.Logf("close error: %v", closeErr)
		}
	}()

	// Same cookies and proxy
	if !c.Matches("__Secure-1PSID=sid1; __Secure-1PSIDTS=ts1", "socks5://127.0.0.1:1080") {
		t.Fatalf("expected Matches to return true for matching config")
	}

	// Different cookie
	if c.Matches("__Secure-1PSID=sid2; __Secure-1PSIDTS=ts1", "socks5://127.0.0.1:1080") {
		t.Fatalf("expected Matches to return false for different cookie")
	}

	// Different proxy
	if c.Matches("__Secure-1PSID=sid1; __Secure-1PSIDTS=ts1", "") {
		t.Fatalf("expected Matches to return false for different proxy")
	}

	// Closed client
	if err := c.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}
	if c.Matches("__Secure-1PSID=sid1; __Secure-1PSIDTS=ts1", "socks5://127.0.0.1:1080") {
		t.Fatalf("expected Matches to return false for closed client")
	}
}

func TestRuntimeInvalidateClient(t *testing.T) {
	rt := &Runtime{
		clients: make(map[string]*Client),
	}

	c1, err := NewClient("__Secure-1PSID=sid1")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	c2, err := NewClient("__Secure-1PSID=sid2")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	rt.clients["acc1"] = c1
	rt.clients["acc2"] = c2

	rt.InvalidateClient("acc1")

	rt.mu.RLock()
	_, found1 := rt.clients["acc1"]
	_, found2 := rt.clients["acc2"]
	rt.mu.RUnlock()

	if found1 {
		t.Fatalf("expected acc1 to be evicted")
	}
	if !found2 {
		t.Fatalf("expected acc2 to remain")
	}
	if !c1.closed {
		t.Fatalf("expected c1 to be closed")
	}

	rt.InvalidateAll()
	rt.mu.RLock()
	total := len(rt.clients)
	rt.mu.RUnlock()
	if total != 0 {
		t.Fatalf("expected all clients to be evicted, got %d", total)
	}
	if !c2.closed {
		t.Fatalf("expected c2 to be closed")
	}
}

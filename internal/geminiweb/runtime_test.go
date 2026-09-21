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

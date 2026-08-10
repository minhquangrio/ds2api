package client

import (
	"testing"

	"ds2api/internal/config"
)

func TestHTTPCloakProxyURLNormalizesSocks5h(t *testing.T) {
	got := httpCloakProxyURL(config.Proxy{
		Type:     "socks5h",
		Host:     "127.0.0.1",
		Port:     1080,
		Username: "user",
		Password: "pass",
	})
	want := "socks5://user:pass@127.0.0.1:1080"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHTTPCloakProxyURLPreservesHTTPAndHTTPS(t *testing.T) {
	gotHTTP := httpCloakProxyURL(config.Proxy{
		Type:     "http",
		Host:     "23.95.159.223",
		Port:     52290,
		Username: "KyxfEp",
		Password: "SihEWZ",
	})
	wantHTTP := "http://KyxfEp:SihEWZ@23.95.159.223:52290"
	if gotHTTP != wantHTTP {
		t.Fatalf("got %q, want %q", gotHTTP, wantHTTP)
	}

	gotHTTPS := httpCloakProxyURL(config.Proxy{
		Type:     "https",
		Host:     "118.70.171.73",
		Port:     33820,
		Username: "reujWU",
		Password: "LvvhJj",
	})
	wantHTTPS := "https://reujWU:LvvhJj@118.70.171.73:33820"
	if gotHTTPS != wantHTTPS {
		t.Fatalf("got %q, want %q", gotHTTPS, wantHTTPS)
	}
}

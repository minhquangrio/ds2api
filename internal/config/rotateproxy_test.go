package config

import (
	"strings"
	"testing"
)

func rotatingEntry(mutate func(*RotateProxyConfig)) Proxy {
	rotate := RotateProxyConfig{Provider: ProxyRotateProviderProxyXoay, Key: "k1"}
	if mutate != nil {
		mutate(&rotate)
	}
	return NormalizeProxy(Proxy{Rotate: &rotate})
}

func TestNormalizeProxyAppliesRotateDefaults(t *testing.T) {
	proxy := rotatingEntry(func(r *RotateProxyConfig) {
		r.Provider = ""
		r.Protocol = "SOCKS5H"
		r.Network = ""
		r.Province = ""
	})
	if proxy.Type != "socks5" {
		t.Fatalf("expected the protocol to drive the type, got %#v", proxy)
	}
	if proxy.Rotate.Provider != ProxyRotateProviderProxyXoay {
		t.Fatalf("unexpected provider: %#v", proxy.Rotate)
	}
	if proxy.Rotate.Network != "random" || proxy.Rotate.Province != "0" {
		t.Fatalf("unexpected defaults: %#v", proxy.Rotate)
	}
	if proxy.Rotate.TTLSeconds != DefaultRotateTTLSeconds || proxy.Rotate.MinRemainingSeconds != DefaultRotateMinRemainingSeconds {
		t.Fatalf("unexpected lease defaults: %#v", proxy.Rotate)
	}
	if proxy.Host != "" || proxy.Port != 0 || proxy.Password != "" {
		t.Fatalf("rotating entry must drop static fields: %#v", proxy)
	}
	if !strings.HasPrefix(proxy.Name, "rotate:") {
		t.Fatalf("expected a generated rotate name, got %q", proxy.Name)
	}
}

func TestNormalizeProxyKeepsExplicitName(t *testing.T) {
	proxy := NormalizeProxy(Proxy{Name: "Xoay VN", Rotate: &RotateProxyConfig{Key: "k"}})
	if proxy.Name != "Xoay VN" {
		t.Fatalf("unexpected name %q", proxy.Name)
	}
}

func TestStableProxyIDSeparatesRotateConfigs(t *testing.T) {
	a := rotatingEntry(func(r *RotateProxyConfig) { r.Key = "different" })
	b := rotatingEntry(nil)
	if a.ID == "" || b.ID == "" {
		t.Fatal("expected generated ids")
	}
	if a.ID == b.ID {
		t.Fatal("different keys must not share a generated proxy id")
	}
	if StableProxyID(rotatingEntry(nil)) != b.ID {
		t.Fatal("the same rotate config must keep a stable id")
	}
}

func TestNormalizeRotateProxyCapsRenewWindow(t *testing.T) {
	rotate := NormalizeRotateProxy(&RotateProxyConfig{Key: "k", TTLSeconds: 30, MinRemainingSeconds: 120})
	if rotate.MinRemainingSeconds >= rotate.TTLSeconds {
		t.Fatalf("renew window must stay below the lease: %#v", rotate)
	}
	if rotate.MinRemainingSeconds != 29 {
		t.Fatalf("unexpected renew window: %#v", rotate)
	}
}

func TestValidateProxyConfigRotating(t *testing.T) {
	valid := rotatingEntry(nil)
	if err := ValidateProxyConfig([]Proxy{valid}); err != nil {
		t.Fatalf("expected a valid rotating entry, got %v", err)
	}

	cases := []struct {
		name   string
		proxy  Proxy
		errHas string
	}{
		{"missing key", rotatingEntry(func(r *RotateProxyConfig) { r.Key = "" }), "rotate.key"},
		{"unknown provider", rotatingEntry(func(r *RotateProxyConfig) { r.Provider = "mystery" }), "rotate.provider"},
		{"custom without endpoint", rotatingEntry(func(r *RotateProxyConfig) { r.Provider = ProxyRotateProviderCustom; r.Endpoint = "" }), "rotate.endpoint"},
		{"bad endpoint", rotatingEntry(func(r *RotateProxyConfig) { r.Endpoint = "ftp://proxy.test" }), "absolute"},
		{"bad whitelist", rotatingEntry(func(r *RotateProxyConfig) { r.Whitelist = "not-an-ip" }), "IPv4"},
		{"ttl out of range", rotatingEntry(func(r *RotateProxyConfig) { r.TTLSeconds = 10_000_000 }), "ttl_seconds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProxyConfig([]Proxy{tc.proxy})
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.errHas)
			}
			if !strings.Contains(err.Error(), tc.errHas) {
				t.Fatalf("error %q does not mention %q", err, tc.errHas)
			}
		})
	}
}

func TestValidateProxyConfigStillChecksStaticEntries(t *testing.T) {
	if err := ValidateProxyConfig([]Proxy{{ID: "p", Type: "socks5", Host: "127.0.0.1", Port: 1080}}); err != nil {
		t.Fatalf("static entry rejected: %v", err)
	}
	if err := ValidateProxyConfig([]Proxy{{ID: "p", Type: "socks5"}}); err == nil {
		t.Fatal("expected host/port validation for a static entry")
	}
	if err := ValidateProxyConfig([]Proxy{{ID: "p", Type: "quic", Host: "h", Port: 1}}); err == nil {
		t.Fatal("expected type validation for a static entry")
	}
}

func TestValidateWhitelistIPv4AcceptsList(t *testing.T) {
	if err := ValidateWhitelistIPv4("1.2.3.4, 5.6.7.8"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateWhitelistIPv4("1.2.3.4,"); err == nil {
		t.Fatal("expected an error for an empty list entry")
	}
	if err := ValidateWhitelistIPv4("::1"); err == nil {
		t.Fatal("expected IPv6 to be rejected")
	}
}

func TestRotateEndpointHost(t *testing.T) {
	if got := RotateEndpointHost("https://proxyxoay.shop/api/get.php?key=1"); got != "proxyxoay.shop" {
		t.Fatalf("unexpected host %q", got)
	}
	if got := RotateEndpointHost(""); got != "" {
		t.Fatalf("unexpected host %q", got)
	}
}

func TestRotateTTLBounds(t *testing.T) {
	if _, ok := RotateTTLBounds(0); ok {
		t.Fatal("zero ttl is not usable")
	}
	if got, ok := RotateTTLBounds(DefaultRotateMaxTTLSeconds + 10); !ok || got != DefaultRotateMaxTTLSeconds {
		t.Fatalf("expected clamping, got %d %v", got, ok)
	}
	if got, ok := RotateTTLBounds(1777); !ok || got != 1777 {
		t.Fatalf("unexpected passthrough %d %v", got, ok)
	}
}

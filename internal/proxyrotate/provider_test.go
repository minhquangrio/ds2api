package proxyrotate

import (
	"testing"
	"time"

	"ds2api/internal/config"
)

func rotateCfg(mutate func(*config.RotateProxyConfig)) config.RotateProxyConfig {
	cfg := config.RotateProxyConfig{Provider: config.ProxyRotateProviderProxyXoay, Key: "k", Network: "random", Province: "0", Protocol: "http"}
	if mutate != nil {
		mutate(&cfg)
	}
	return *config.NormalizeRotateProxy(&cfg)
}

func TestRequestURLUsesDocumentedParameters(t *testing.T) {
	cfg := rotateCfg(func(c *config.RotateProxyConfig) {
		c.Network = "fpt"
		c.Province = "10"
		c.Whitelist = "1.2.3.4"
	})
	got, err := RequestURL(cfg)
	if err != nil {
		t.Fatalf("RequestURL: %v", err)
	}
	want := "https://proxyxoay.shop/api/get.php?key=k&nhamang=fpt&tinhthanh=10&whitelist=1.2.3.4"
	if got != want {
		t.Fatalf("URL mismatch\n got %s\nwant %s", got, want)
	}
}

func TestRequestURLSendsEmptyOptionalParameters(t *testing.T) {
	got, err := RequestURL(rotateCfg(nil))
	if err != nil {
		t.Fatalf("RequestURL: %v", err)
	}
	want := "https://proxyxoay.shop/api/get.php?key=k&nhamang=random&tinhthanh=0&whitelist="
	if got != want {
		t.Fatalf("URL mismatch\n got %s\nwant %s", got, want)
	}
}

func TestRequestURLCustomEndpoint(t *testing.T) {
	cfg := rotateCfg(func(c *config.RotateProxyConfig) {
		c.Provider = config.ProxyRotateProviderCustom
		c.Endpoint = "https://example.test/api/get"
	})
	got, err := RequestURL(cfg)
	if err != nil {
		t.Fatalf("RequestURL: %v", err)
	}
	if got != "https://example.test/api/get?key=k&nhamang=random&tinhthanh=0&whitelist=" {
		t.Fatalf("unexpected url %s", got)
	}
}

func TestParseProviderResponseSuccess(t *testing.T) {
	body := []byte(`{
		"status": 100,
		"message": "proxy nay se die sau 1777s",
		"proxyhttp": "42.117.243.215:10836::",
		"proxysocks5": "42.117.243.215:30836::",
		"Nha Mang": "fpt",
		"Vi Tri": "HaNoi1",
		"Token expiration date": "22:52 19-02-2025"
	}`)
	now := time.Date(2025, 2, 19, 10, 0, 0, 0, time.Local)

	resolved, err := ParseProviderResponse(body, rotateCfg(nil), now)
	if err != nil {
		t.Fatalf("ParseProviderResponse: %v", err)
	}
	if resolved.proxy.Type != "http" || resolved.proxy.Host != "42.117.243.215" || resolved.proxy.Port != 10836 {
		t.Fatalf("unexpected proxy: %+v", resolved.proxy)
	}
	if resolved.network != "fpt" || resolved.location != "HaNoi1" {
		t.Fatalf("unexpected metadata: %+v", resolved)
	}
	if want := now.Add(1777 * time.Second); !resolved.expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", resolved.expiresAt, want)
	}
}

func TestParseProviderResponsePrefersSocks5WhenConfigured(t *testing.T) {
	body := []byte(`{"status":100,"message":"proxy nay se die sau 60s","proxyhttp":"1.1.1.1:10836::","proxysocks5":"1.1.1.1:30836::"}`)
	resolved, err := ParseProviderResponse(body, rotateCfg(func(c *config.RotateProxyConfig) { c.Protocol = "socks5" }), time.Now())
	if err != nil {
		t.Fatalf("ParseProviderResponse: %v", err)
	}
	if resolved.proxy.Type != "socks5" || resolved.proxy.Port != 30836 {
		t.Fatalf("unexpected proxy: %+v", resolved.proxy)
	}
}

func TestParseProviderResponseFallsBackToHTTPWhenNoSocks5(t *testing.T) {
	body := []byte(`{"status":100,"message":"ok","proxyhttp":"1.1.1.1:8080::"}`)
	resolved, err := ParseProviderResponse(body, rotateCfg(func(c *config.RotateProxyConfig) { c.Protocol = "socks5" }), time.Now())
	if err != nil {
		t.Fatalf("ParseProviderResponse: %v", err)
	}
	if resolved.proxy.Type != "http" || resolved.proxy.Port != 8080 {
		t.Fatalf("unexpected proxy: %+v", resolved.proxy)
	}
}

func TestParseProviderResponseKeepsCredentials(t *testing.T) {
	body := []byte(`{"status":100,"message":"ok","proxyhttp":"1.1.1.1:8080:user:pass"}`)
	resolved, err := ParseProviderResponse(body, rotateCfg(nil), time.Now())
	if err != nil {
		t.Fatalf("ParseProviderResponse: %v", err)
	}
	if resolved.proxy.Username != "user" || resolved.proxy.Password != "pass" {
		t.Fatalf("unexpected credentials: %+v", resolved.proxy)
	}
}

func TestParseProviderResponseErrorStatuses(t *testing.T) {
	for _, body := range []string{
		`{"status":101,"comen":"key does not exist"}`,
		`{"status":102,"message":"key khong ton tai"}`,
	} {
		_, err := ParseProviderResponse([]byte(body), rotateCfg(nil), time.Now())
		if err == nil {
			t.Fatalf("expected error for %s", body)
		}
	}
}

func TestParseProviderResponseRejectsGarbage(t *testing.T) {
	_, err := ParseProviderResponse([]byte("<html>blocked</html>"), rotateCfg(nil), time.Now())
	if err == nil {
		t.Fatal("expected error for non-JSON body")
	}
}

func TestParseProviderResponseUsesConfiguredTTL(t *testing.T) {
	body := []byte(`{"status":100,"message":"ok","proxyhttp":"1.1.1.1:8080::"}`)
	now := time.Now()
	resolved, err := ParseProviderResponse(body, rotateCfg(func(c *config.RotateProxyConfig) { c.TTLSeconds = 42 }), now)
	if err != nil {
		t.Fatalf("ParseProviderResponse: %v", err)
	}
	if want := now.Add(42 * time.Second); resolved.expiresAt.Sub(want) > time.Second {
		t.Fatalf("expiresAt = %v, want ~%v", resolved.expiresAt, want)
	}
}

func TestParseProviderResponseCapsLeaseAtKeyExpiration(t *testing.T) {
	body := []byte(`{"status":100,"message":"proxy nay se die sau 1777s","proxyhttp":"1.1.1.1:8080::","Token expiration date":"10:05 19-02-2025"}`)
	now := time.Date(2025, 2, 19, 10, 0, 0, 0, time.Local)
	resolved, err := ParseProviderResponse(body, rotateCfg(nil), now)
	if err != nil {
		t.Fatalf("ParseProviderResponse: %v", err)
	}
	if want := now.Add(5 * time.Minute); !resolved.expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", resolved.expiresAt, want)
	}
}

func TestParseEndpointRejectsMalformedValues(t *testing.T) {
	for _, raw := range []string{"", "1.1.1.1", "1.1.1.1:notaport", "not-an-ip:8080", "1.1.1.1:99999"} {
		if _, _, _, _, err := parseEndpoint(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
	host, port, user, pass, err := parseEndpoint("9.9.9.9:80::")
	if err != nil {
		t.Fatalf("parseEndpoint: %v", err)
	}
	if host != "9.9.9.9" || port != 80 || user != "" || pass != "" {
		t.Fatalf("unexpected parse: %s %d %q %q", host, port, user, pass)
	}
}

func TestTTLFromMessageVariants(t *testing.T) {
	cases := map[string]int{
		"proxy nay se die sau 1777s": 1777,
		"expires in 60 seconds":      60,
		"còn 300s":                   300,
		"no lifetime here":           0,
	}
	for message, want := range cases {
		got, ok := ttlFromMessage(message)
		if want == 0 {
			if ok {
				t.Fatalf("expected no ttl for %q", message)
			}
			continue
		}
		if !ok || got != want {
			t.Fatalf("ttlFromMessage(%q) = %d,%v want %d", message, got, ok, want)
		}
	}
}

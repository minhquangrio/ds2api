package shared

import (
	"strings"
	"testing"

	"ds2api/internal/config"
)

func TestProxyViewStaticKeepsAddress(t *testing.T) {
	view := ProxyView(config.Proxy{ID: "s", Type: "socks5h", Host: "127.0.0.1", Port: 1080, Password: "secret"})
	if view["mode"] != "static" {
		t.Fatalf("expected static mode, got %#v", view)
	}
	if view["host"] != "127.0.0.1" || view["port"] != 1080 {
		t.Fatalf("unexpected address: %#v", view)
	}
	if view["has_password"] != true {
		t.Fatalf("expected the password marker, got %#v", view)
	}
	if _, found := view["password"]; found {
		t.Fatal("the password must not be exposed")
	}
	if _, found := view["rotate"]; found {
		t.Fatal("a static entry must not carry a rotate block")
	}
}

func TestProxyViewRotateHidesSecretsAndStaticFields(t *testing.T) {
	proxy := config.NormalizeProxy(config.Proxy{ID: "px", Rotate: &config.RotateProxyConfig{
		Provider: config.ProxyRotateProviderCustom,
		Endpoint: "https://user:pass@proxy.test/api/get.php?key=embedded-secret",
		Key:      "keyxoay-secret",
	}})
	view := ProxyView(proxy)
	if view["mode"] != "rotate" {
		t.Fatalf("expected rotate mode, got %#v", view)
	}
	if view["host"] != "" || view["port"] != 0 {
		t.Fatalf("rotating entries must not surface a static address: %#v", view)
	}
	block, ok := view["rotate"].(map[string]any)
	if !ok {
		t.Fatalf("missing rotate block: %#v", view)
	}
	if block["has_key"] != true {
		t.Fatalf("expected has_key, got %#v", block)
	}
	if block["key_preview"] == "keyxoay-secret" {
		t.Fatal("the rotate key must be masked")
	}
	endpoint, _ := block["endpoint"].(string)
	if endpoint == "" {
		t.Fatal("expected an endpoint preview")
	}
	for _, leak := range []string{"keyxoay-secret", "embedded-secret", "pass@"} {
		preview, _ := block["key_preview"].(string)
		if strings.Contains(endpoint, leak) || strings.Contains(preview, leak) {
			t.Fatalf("preview leaked %q: %#v", leak, block)
		}
	}
}

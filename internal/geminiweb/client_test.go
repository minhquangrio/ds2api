package geminiweb

import (
	"testing"
	"time"
)

func TestParseCookiesSemicolonString(t *testing.T) {
	raw := "__Secure-1PSID=psid_val; __Secure-1PSIDTS=psidts_val; other=abc"
	m, header, err := ParseCookies(raw)
	if err != nil {
		t.Fatalf("ParseCookies failed: %v", err)
	}
	if m["__Secure-1PSID"] != "psid_val" {
		t.Errorf("expected psid_val, got %s", m["__Secure-1PSID"])
	}
	if m["__Secure-1PSIDTS"] != "psidts_val" {
		t.Errorf("expected psidts_val, got %s", m["__Secure-1PSIDTS"])
	}
	if header == "" {
		t.Errorf("expected non-empty cookie header")
	}
}

func TestParseCookiesJSONMap(t *testing.T) {
	raw := `{"__Secure-1PSID": "psid_json", "__Secure-1PSIDTS": "ts_json"}`
	m, _, err := ParseCookies(raw)
	if err != nil {
		t.Fatalf("ParseCookies failed: %v", err)
	}
	if m["__Secure-1PSID"] != "psid_json" {
		t.Errorf("expected psid_json, got %s", m["__Secure-1PSID"])
	}
}

func TestParseCookiesJSONArray(t *testing.T) {
	raw := `[
		{"name": "__Secure-1PSID", "value": "psid_arr"},
		{"name": "__Secure-1PSIDTS", "value": "ts_arr"}
	]`
	m, _, err := ParseCookies(raw)
	if err != nil {
		t.Fatalf("ParseCookies failed: %v", err)
	}
	if m["__Secure-1PSID"] != "psid_arr" {
		t.Errorf("expected psid_arr, got %s", m["__Secure-1PSID"])
	}
}

func TestParseCookiesInvalid(t *testing.T) {
	_, _, err := ParseCookies("")
	if err == nil {
		t.Errorf("expected error for empty cookies")
	}
}

func TestGeminiClientTimeout(t *testing.T) {
	t.Run("DefaultTimeoutIs300s", func(t *testing.T) {
		t.Setenv("DS2API_GEMINI_TIMEOUT_SECONDS", "")
		if got := geminiClientTimeout(); got != 300*time.Second {
			t.Fatalf("expected 300s, got %v", got)
		}
	})

	t.Run("EnvTimeoutCustom", func(t *testing.T) {
		t.Setenv("DS2API_GEMINI_TIMEOUT_SECONDS", "180")
		if got := geminiClientTimeout(); got != 180*time.Second {
			t.Fatalf("expected 180s, got %v", got)
		}
	})

	t.Run("EnvTimeoutInvalidFallsBack", func(t *testing.T) {
		t.Setenv("DS2API_GEMINI_TIMEOUT_SECONDS", "abc")
		if got := geminiClientTimeout(); got != 300*time.Second {
			t.Fatalf("expected 300s, got %v", got)
		}
	})

	t.Run("EnvTimeoutZeroFallsBack", func(t *testing.T) {
		t.Setenv("DS2API_GEMINI_TIMEOUT_SECONDS", "0")
		if got := geminiClientTimeout(); got != 300*time.Second {
			t.Fatalf("expected 300s, got %v", got)
		}
	})
}

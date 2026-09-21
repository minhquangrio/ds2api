package geminiweb

import (
	"testing"
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

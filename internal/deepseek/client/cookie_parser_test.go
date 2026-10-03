package client

import (
	"testing"
)

func TestParseDeepSeekSession_PureJWT(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN"
	res, err := ParseDeepSeekSession(jwt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Token != jwt {
		t.Errorf("got token %q, want %q", res.Token, jwt)
	}
	if res.CookieHeader != "" {
		t.Errorf("got cookieHeader %q, want empty", res.CookieHeader)
	}
}

func TestParseDeepSeekSession_BearerJWT(t *testing.T) {
	jwt := "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN"
	res, err := ParseDeepSeekSession(jwt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN"
	if res.Token != want {
		t.Errorf("got token %q, want %q", res.Token, want)
	}
}

func TestParseDeepSeekSession_ConsoleJSONExport(t *testing.T) {
	raw := `{"token":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN","cookies":"ds_session_id=sess123; HWWAFSESTIME=172000"}`
	res, err := ParseDeepSeekSession(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN"
	if res.Token != wantToken {
		t.Errorf("got token %q, want %q", res.Token, wantToken)
	}
	if res.CookiesMap["ds_session_id"] != "sess123" {
		t.Errorf("got ds_session_id %q, want sess123", res.CookiesMap["ds_session_id"])
	}
	if res.CookiesMap["HWWAFSESTIME"] != "172000" {
		t.Errorf("got HWWAFSESTIME %q, want 172000", res.CookiesMap["HWWAFSESTIME"])
	}
}

func TestParseDeepSeekSession_CookieEditorArray(t *testing.T) {
	raw := `[
		{"name": "ds_session_id", "value": "sess456"},
		{"name": "userToken", "value": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN"},
		{"name": "_ga", "value": "GA1.2.3"}
	]`
	res, err := ParseDeepSeekSession(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN"
	if res.Token != wantToken {
		t.Errorf("got token %q, want %q", res.Token, wantToken)
	}
	if res.CookiesMap["ds_session_id"] != "sess456" {
		t.Errorf("got ds_session_id %q, want sess456", res.CookiesMap["ds_session_id"])
	}
	if res.CookiesMap["_ga"] != "GA1.2.3" {
		t.Errorf("got _ga %q, want GA1.2.3", res.CookiesMap["_ga"])
	}
}

func TestParseDeepSeekSession_CookieHeaderString(t *testing.T) {
	raw := "ds_session_id=sess789; userToken=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN; cf_clearance=clear123"
	res, err := ParseDeepSeekSession(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgN"
	if res.Token != wantToken {
		t.Errorf("got token %q, want %q", res.Token, wantToken)
	}
	if res.CookiesMap["ds_session_id"] != "sess789" {
		t.Errorf("got ds_session_id %q, want sess789", res.CookiesMap["ds_session_id"])
	}
	if res.CookiesMap["cf_clearance"] != "clear123" {
		t.Errorf("got cf_clearance %q, want clear123", res.CookiesMap["cf_clearance"])
	}
}

func TestParseDeepSeekSession_Empty(t *testing.T) {
	_, err := ParseDeepSeekSession("   ")
	if err == nil {
		t.Fatal("expected error for empty string, got nil")
	}
}

func TestParseDeepSeekSession_WrappedJSONToken(t *testing.T) {
	raw := `{"token":"{\"value\":\"/zVuNHSdX0sdi+X0oSzD0otMgOZAwaXs+AIq4tYYyPoANfh4KbT17g3nBTcSV3AF\",\"__version\":\"0\"}","cookies":"smidV2=2025092620382621098f218c3ceb2e0e1b108a3aa54c120065d3f7c4b5b1930; aws-waf-token=9e52fb6d"}`
	res, err := ParseDeepSeekSession(raw)
	if err != nil {
		t.Fatalf("ParseDeepSeekSession failed: %v", err)
	}
	wantToken := "/zVuNHSdX0sdi+X0oSzD0otMgOZAwaXs+AIq4tYYyPoANfh4KbT17g3nBTcSV3AF"
	if res.Token != wantToken {
		t.Errorf("got token %q, want %q", res.Token, wantToken)
	}
	if res.CookiesMap["smidV2"] != "2025092620382621098f218c3ceb2e0e1b108a3aa54c120065d3f7c4b5b1930" {
		t.Errorf("got smidV2 %q", res.CookiesMap["smidV2"])
	}
	if res.CookiesMap["aws-waf-token"] != "9e52fb6d" {
		t.Errorf("got aws-waf-token %q", res.CookiesMap["aws-waf-token"])
	}
}

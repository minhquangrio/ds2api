package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/config"
)

func TestParseAuthorizationInputVariants(t *testing.T) {
	cases := []struct {
		in        string
		wantCode  string
		wantState string
	}{
		{"http://localhost:1455/auth/callback?code=abc12345&state=st999", "abc12345", "st999"},
		{"https://chatgpt.com/?code=abc12345&state=st999", "abc12345", "st999"},
		{"code=abc12345&state=st999", "abc12345", "st999"},
		{"abc12345#st999", "abc12345", "st999"},
		{"rawcode123", "rawcode123", ""},
		{"   ", "", ""},
	}

	for _, c := range cases {
		gotCode, gotState := ParseAuthorizationInput(c.in)
		if gotCode != c.wantCode || gotState != c.wantState {
			t.Errorf("ParseAuthorizationInput(%q) = (%q, %q); want (%q, %q)", c.in, gotCode, gotState, c.wantCode, c.wantState)
		}
	}
}

func TestExtractIdentityJWT(t *testing.T) {
	payload := map[string]any{
		"email": "user@example.com",
		AuthClaimOpenAI: map[string]any{
			"chatgpt_account_id": "acc_xyz789",
			"plan_type":          "plus",
		},
	}
	payloadBytes, _ := json.Marshal(payload)
	b64 := base64.RawURLEncoding.EncodeToString(payloadBytes)
	fakeJWT := "header." + b64 + ".signature"

	email, accID, planType := ExtractIdentity(fakeJWT, "")
	if email != "user@example.com" {
		t.Errorf("email = %q; want user@example.com", email)
	}
	if accID != "acc_xyz789" {
		t.Errorf("accID = %q; want acc_xyz789", accID)
	}
	if planType != "plus" {
		t.Errorf("planType = %q; want plus", planType)
	}
}

func TestSessionManager(t *testing.T) {
	mgr := DefaultLoginManager()
	// Use port 0 or high unused port for ephemeral session
	session, err := mgr.StartSession(21455)
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}
	defer session.closeServer()

	if session.AuthorizeURL == "" || session.State == "" {
		t.Fatalf("session uninitialized: %+v", session)
	}

	fetched, ok := mgr.GetSession(session.ID)
	if !ok || fetched.ID != session.ID {
		t.Fatalf("GetSession failed")
	}

	if err := mgr.CancelSession(session.ID); err != nil {
		t.Fatalf("CancelSession failed: %v", err)
	}
	if got := session.Status(); got != "cancelled" {
		t.Errorf("session status = %s; want cancelled", got)
	}
}

func TestExchangeAndRefreshMocked(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access_mock_123",
			"refresh_token": "refresh_mock_456",
			"token_type":    "bearer",
			"expires_in":    3600,
		})
	}))
	defer ts.Close()

	// Direct test of exchange parsing logic with custom URL if needed
	ctx := context.Background()
	_ = ctx
}

// TestLoginSessionViewRedactsTokens ensures the polled session view never
// carries credentials back to the console.
func TestLoginSessionViewRedactsTokens(t *testing.T) {
	s := &LoginSession{
		ID:           "codex_login_1",
		AuthorizeURL: "https://auth.openai.com/oauth/authorize?x=1",
		RedirectURI:  "http://localhost:1455/auth/callback",
		State:        "state123",
		status:       "completed",
		account: &config.Account{
			Provider:          "codex",
			Email:             "user@example.com",
			Name:              "codex:acc-1",
			Token:             "access-secret",
			CodexRefreshToken: "refresh-secret",
			CodexIDToken:      "id-secret",
			CodexAccountID:    "acc-1",
			CodexPlanType:     "plus",
		},
	}

	view := s.View()
	if view.Status != "completed" {
		t.Fatalf("status = %q; want completed", view.Status)
	}
	if view.Account == nil {
		t.Fatal("expected account in view")
	}
	if view.Account.AccountID != "acc-1" || !view.Account.HasRefreshTok {
		t.Errorf("unexpected sanitized account: %+v", view.Account)
	}

	// The view struct has no token fields at all; assert by serialization so a
	// future field addition cannot silently reintroduce the leak.
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal view: %v", err)
	}
	for _, secret := range []string{"access-secret", "refresh-secret", "id-secret"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("session view leaked %q: %s", secret, string(raw))
		}
	}
}

package codex

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
)

type mockPool struct{}

func (m *mockPool) Reset()                                                                        {}
func (m *mockPool) Status() map[string]any                                                        { return nil }
func (m *mockPool) ApplyRuntimeLimits(maxInflightPerAccount, maxQueueSize, globalMaxInflight int) {}

func TestCodexAdminHandlers(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{
		"accounts": [
			{
				"provider": "codex",
				"email": "codex-test@example.com",
				"token": "tok_1234567890",
				"codex_account_id": "acc-123",
				"codex_plan_type": "plus",
				"codex_refresh_token": "ref_123"
			}
		]
	}`)

	store := config.LoadStore()
	h := &Handler{
		Store: store,
		Pool:  &mockPool{},
	}

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	t.Run("listAccounts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/codex/accounts", nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rec.Code)
		}

		var accounts []CodexAccountView
		if err := json.Unmarshal(rec.Body.Bytes(), &accounts); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if len(accounts) != 1 {
			t.Fatalf("expected 1 account, got %d", len(accounts))
		}
		if accounts[0].AccountID != "acc-123" {
			t.Errorf("expected acc-123, got %s", accounts[0].AccountID)
		}
	})

	t.Run("startLogin", func(t *testing.T) {
		body := bytes.NewBufferString(`{"port": 1459}`)
		req := httptest.NewRequest(http.MethodPost, "/codex/login/start", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		sessionID, ok := resp["session_id"].(string)
		if !ok || sessionID == "" {
			t.Fatalf("expected session_id in response")
		}

		// Test poll login
		pollBody := bytes.NewBufferString(`{"session_id": "` + sessionID + `"}`)
		pollReq := httptest.NewRequest(http.MethodPost, "/codex/login/poll", pollBody)
		pollRec := httptest.NewRecorder()
		r.ServeHTTP(pollRec, pollReq)

		if pollRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", pollRec.Code)
		}

		// Test cancel login
		cancelBody := bytes.NewBufferString(`{"session_id": "` + sessionID + `"}`)
		cancelReq := httptest.NewRequest(http.MethodPost, "/codex/login/cancel", cancelBody)
		cancelRec := httptest.NewRecorder()
		r.ServeHTTP(cancelRec, cancelReq)

		if cancelRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", cancelRec.Code)
		}
	})

	t.Run("deleteAccount", func(t *testing.T) {
		delBody := bytes.NewBufferString(`{"account_id": "acc-123"}`)
		delReq := httptest.NewRequest(http.MethodDelete, "/codex/accounts", delBody)
		delRec := httptest.NewRecorder()
		r.ServeHTTP(delRec, delReq)

		if delRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", delRec.Code, delRec.Body.String())
		}

		// Verify account is removed
		accounts := h.Store.Accounts()
		for _, acc := range accounts {
			if acc.CodexAccountID == "acc-123" {
				t.Fatalf("account acc-123 was not deleted")
			}
		}
	})
}

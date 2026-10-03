package codex

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ds2api/internal/codex"
	"ds2api/internal/config"
)

type StartLoginRequest struct {
	Port int `json:"port"`
}

type PollLoginRequest struct {
	SessionID string `json:"session_id"`
}

type CompleteLoginRequest struct {
	SessionID   string `json:"session_id"`
	Code        string `json:"code"`
	RedirectURL string `json:"redirect_url"`
}

type CancelLoginRequest struct {
	SessionID string `json:"session_id"`
}

func (h *Handler) startLogin(w http.ResponseWriter, r *http.Request) {
	var req StartLoginRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	session, err := codex.DefaultLoginManager().StartSession(req.Port)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":         session.ID,
		"authorize_url":      session.AuthorizeURL,
		"redirect_uri":       session.RedirectURI,
		"state":              session.State,
		"expires_in_seconds": session.ExpiresInSeconds,
	})
}

func (h *Handler) pollLogin(w http.ResponseWriter, r *http.Request) {
	var req PollLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	session, ok := codex.DefaultLoginManager().GetSession(req.SessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Persist once, on the first poll that observes completion.
	if session.Status() == "completed" && session.ClaimPersisted() && h.Store != nil {
		if acc := session.Account(); acc != nil {
			if err := h.saveOrUpdateAccount(*acc); err != nil {
				http.Error(w, "failed to save account: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, session.View())
}

func (h *Handler) completeLogin(w http.ResponseWriter, r *http.Request) {
	var req CompleteLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	var err error
	if strings.TrimSpace(req.RedirectURL) != "" {
		err = codex.DefaultLoginManager().CompleteSessionWithInput(req.SessionID, req.RedirectURL)
	} else if strings.TrimSpace(req.Code) != "" {
		err = codex.DefaultLoginManager().CompleteSessionWithCode(req.SessionID, req.Code)
	} else {
		http.Error(w, "code or redirect_url is required", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	session, ok := codex.DefaultLoginManager().GetSession(req.SessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusInternalServerError)
		return
	}
	acc := session.Account()
	if acc == nil {
		http.Error(w, "session account not found", http.StatusInternalServerError)
		return
	}

	if h.Store != nil {
		if err := h.saveOrUpdateAccount(*acc); err != nil {
			http.Error(w, "failed to save account: "+err.Error(), http.StatusInternalServerError)
			return
		}
		session.ClaimPersisted()
	}

	writeJSON(w, http.StatusOK, session.View())
}

func (h *Handler) cancelLogin(w http.ResponseWriter, r *http.Request) {
	var req CancelLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	if err := codex.DefaultLoginManager().CancelSession(req.SessionID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

func (h *Handler) saveOrUpdateAccount(acc config.Account) error {
	if h.Store == nil {
		return errors.New("config store not available")
	}

	err := h.Store.Update(func(cfg *config.Config) error {
		found := false
		for i, existing := range cfg.Accounts {
			// Match by codex_account_id or email
			if existing.IsCodex() {
				if (acc.CodexAccountID != "" && existing.CodexAccountID == acc.CodexAccountID) ||
					(acc.Email != "" && existing.Email == acc.Email) {
					cfg.Accounts[i].Token = acc.Token
					cfg.Accounts[i].CodexRefreshToken = acc.CodexRefreshToken
					cfg.Accounts[i].CodexIDToken = acc.CodexIDToken
					cfg.Accounts[i].CodexExpiresAt = acc.CodexExpiresAt
					cfg.Accounts[i].CodexPlanType = acc.CodexPlanType
					cfg.Accounts[i].CodexAccountID = acc.CodexAccountID
					cfg.Accounts[i].CodexAccountSource = acc.CodexAccountSource
					cfg.Accounts[i].Disabled = false
					found = true
					break
				}
			}
		}
		if !found {
			cfg.Accounts = append(cfg.Accounts, acc)
		}
		return nil
	})

	if err == nil && h.Pool != nil {
		h.Pool.Reset()
	}
	return err
}

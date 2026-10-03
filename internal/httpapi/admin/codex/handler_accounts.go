package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/codex"
	"ds2api/internal/config"
	adminshared "ds2api/internal/httpapi/admin/shared"
)

type AccountTestRequest struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}

type AccountRefreshRequest struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}

type AccountDeleteRequest struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}

type CodexAccountView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Provider      string `json:"provider"`
	AccountID     string `json:"codex_account_id"`
	PlanType      string `json:"codex_plan_type"`
	AccountSource string `json:"codex_account_source"`
	ExpiresAt     int64  `json:"codex_expires_at"`
	IsExpired     bool   `json:"is_expired"`
	Disabled      bool   `json:"disabled"`
	TokenPreview  string `json:"token_preview"`
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		writeJSON(w, http.StatusOK, []CodexAccountView{})
		return
	}

	accounts := h.Store.Accounts()
	var result []CodexAccountView
	now := time.Now().Unix()

	for _, acc := range accounts {
		if !acc.IsCodex() {
			continue
		}
		isExpired := acc.CodexExpiresAt > 0 && acc.CodexExpiresAt <= now
		result = append(result, CodexAccountView{
			ID:            acc.CodexAccountID,
			Name:          acc.Name,
			Email:         acc.Email,
			Provider:      acc.Provider,
			AccountID:     acc.CodexAccountID,
			PlanType:      acc.CodexPlanType,
			AccountSource: acc.CodexAccountSource,
			ExpiresAt:     acc.CodexExpiresAt,
			IsExpired:     isExpired,
			Disabled:      acc.Disabled,
			TokenPreview:  adminshared.MaskSecretPreview(acc.Token),
		})
	}

	if result == nil {
		result = []CodexAccountView{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) refreshAccount(w http.ResponseWriter, r *http.Request) {
	var req AccountRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.AccountID == "" && req.Email == "" {
		http.Error(w, "account_id or email is required", http.StatusBadRequest)
		return
	}

	if h.Store == nil {
		http.Error(w, "config store not available", http.StatusInternalServerError)
		return
	}

	var targetAcc *config.Account
	for _, acc := range h.Store.Accounts() {
		if acc.IsCodex() {
			if (req.AccountID != "" && acc.CodexAccountID == req.AccountID) ||
				(req.Email != "" && strings.EqualFold(acc.Email, req.Email)) {
				cp := acc
				targetAcc = &cp
				break
			}
		}
	}

	if targetAcc == nil {
		http.Error(w, "codex account not found", http.StatusNotFound)
		return
	}

	if targetAcc.CodexRefreshToken == "" {
		http.Error(w, "no refresh token available for this account", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tokens, err := codex.RefreshCredentials(ctx, targetAcc.CodexRefreshToken)
	if err != nil {
		http.Error(w, fmt.Sprintf("refresh failed: %v", err), http.StatusBadGateway)
		return
	}

	expiresAt := time.Now().Unix() + tokens.ExpiresIn
	err = h.Store.Update(func(cfg *config.Config) error {
		for i, a := range cfg.Accounts {
			if a.IsCodex() && ((targetAcc.CodexAccountID != "" && a.CodexAccountID == targetAcc.CodexAccountID) ||
				(targetAcc.Email != "" && strings.EqualFold(a.Email, targetAcc.Email))) {
				cfg.Accounts[i].Token = tokens.AccessToken
				if tokens.RefreshToken != "" {
					cfg.Accounts[i].CodexRefreshToken = tokens.RefreshToken
				}
				if tokens.IDToken != "" {
					cfg.Accounts[i].CodexIDToken = tokens.IDToken
				}
				cfg.Accounts[i].CodexExpiresAt = expiresAt
				break
			}
		}
		return nil
	})

	if err != nil {
		http.Error(w, "failed to update account in store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.Pool != nil {
		h.Pool.Reset()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"expires_at": expiresAt,
	})
}

func (h *Handler) testAccount(w http.ResponseWriter, r *http.Request) {
	var req AccountTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if h.Store == nil {
		http.Error(w, "config store not available", http.StatusInternalServerError)
		return
	}

	var targetAcc *config.Account
	for _, acc := range h.Store.Accounts() {
		if acc.IsCodex() {
			if (req.AccountID != "" && acc.CodexAccountID == req.AccountID) ||
				(req.Email != "" && strings.EqualFold(acc.Email, req.Email)) {
				cp := acc
				targetAcc = &cp
				break
			}
		}
	}

	if targetAcc == nil {
		http.Error(w, "codex account not found", http.StatusNotFound)
		return
	}

	// Auto refresh if expired or near expiry
	token := targetAcc.Token
	now := time.Now().Unix()
	if targetAcc.CodexExpiresAt > 0 && targetAcc.CodexExpiresAt <= now+60 && targetAcc.CodexRefreshToken != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		tokens, err := codex.RefreshCredentials(ctx, targetAcc.CodexRefreshToken)
		cancel()
		if err == nil && tokens != nil {
			token = tokens.AccessToken
			_ = h.Store.Update(func(cfg *config.Config) error {
				for i, a := range cfg.Accounts {
					if a.IsCodex() && a.CodexAccountID == targetAcc.CodexAccountID {
						cfg.Accounts[i].Token = tokens.AccessToken
						if tokens.RefreshToken != "" {
							cfg.Accounts[i].CodexRefreshToken = tokens.RefreshToken
						}
						cfg.Accounts[i].CodexExpiresAt = time.Now().Unix() + tokens.ExpiresIn
						break
					}
				}
				return nil
			})
		}
	}

	start := time.Now()
	client := codex.NewClient(token, targetAcc.CodexAccountID, "")
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, codex.CodexModelsURL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	resp, err := client.Do(httpReq)
	duration := time.Since(start)

	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     false,
			"error":       err.Error(),
			"duration_ms": duration.Milliseconds(),
		})
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		writeJSON(w, http.StatusOK, map[string]any{
			"success":     false,
			"status_code": resp.StatusCode,
			"error":       fmt.Sprintf("upstream returned status %d", resp.StatusCode),
			"duration_ms": duration.Milliseconds(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"status_code": resp.StatusCode,
		"duration_ms": duration.Milliseconds(),
	})
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var req AccountDeleteRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if req.AccountID == "" {
		req.AccountID = r.URL.Query().Get("account_id")
	}
	if req.Email == "" {
		req.Email = r.URL.Query().Get("email")
	}

	if req.AccountID == "" && req.Email == "" {
		http.Error(w, "account_id or email is required", http.StatusBadRequest)
		return
	}

	if h.Store == nil {
		http.Error(w, "config store not available", http.StatusInternalServerError)
		return
	}

	deleted := false
	err := h.Store.Update(func(cfg *config.Config) error {
		var filtered []config.Account
		for _, a := range cfg.Accounts {
			if a.IsCodex() && ((req.AccountID != "" && a.CodexAccountID == req.AccountID) ||
				(req.Email != "" && strings.EqualFold(a.Email, req.Email))) {
				deleted = true
				continue
			}
			filtered = append(filtered, a)
		}
		cfg.Accounts = filtered
		return nil
	})

	if err != nil {
		http.Error(w, "failed to update config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !deleted {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}

	if h.Pool != nil {
		h.Pool.Reset()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

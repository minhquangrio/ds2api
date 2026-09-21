package accounts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	"ds2api/internal/geminiweb"
)

func (h *Handler) getAccountQuota(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	if decoded, err := url.PathUnescape(identifier); err == nil {
		identifier = decoded
	}

	acc, ok := findAccountByIdentifier(h.Store, identifier)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "账号不存在"})
		return
	}

	if !acc.IsGemini() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "仅支持查询 Gemini 账号的配额与用量"})
		return
	}

	if strings.TrimSpace(acc.Cookies) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Gemini 账号未配置 cookies"})
		return
	}

	client, err := geminiweb.DefaultRuntime().GetClient(r.Context(), acc, h.Store)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"detail": fmt.Sprintf("Gemini 客户端初始化失败: %v", err)})
		return
	}

	forceRefresh := r.URL.Query().Get("refresh") == "true"
	quotaSummary, err := client.GetFullQuota(r.Context(), forceRefresh)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"detail": fmt.Sprintf("获取 Gemini 配额失败: %v", err)})
		return
	}

	quotaSummary.Identifier = acc.Identifier()
	writeJSON(w, http.StatusOK, quotaSummary)
}

func (h *Handler) getAllGeminiQuotas(w http.ResponseWriter, r *http.Request) {
	accounts := h.Store.Snapshot().Accounts
	var geminiAccounts []config.Account
	for _, acc := range accounts {
		if acc.IsGemini() {
			geminiAccounts = append(geminiAccounts, acc)
		}
	}

	if len(geminiAccounts) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"total":    0,
			"accounts": []any{},
		})
		return
	}

	forceRefresh := r.URL.Query().Get("refresh") == "true"
	type itemResult struct {
		Identifier string                               `json:"identifier"`
		Name       string                               `json:"name"`
		Email      string                               `json:"email,omitempty"`
		Enabled    bool                                 `json:"enabled"`
		Muted      bool                                 `json:"muted"`
		Quota      *geminiweb.GeminiAccountQuotaSummary `json:"quota,omitempty"`
		Error      string                               `json:"error,omitempty"`
	}

	results := make([]itemResult, len(geminiAccounts))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	for i, acc := range geminiAccounts {
		wg.Add(1)
		go func(idx int, a config.Account) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := itemResult{
				Identifier: a.Identifier(),
				Name:       a.Name,
				Email:      a.Email,
				Enabled:    a.IsEnabled(),
				Muted:      a.IsMuted(),
			}

			if strings.TrimSpace(a.Cookies) == "" {
				res.Error = "cookies not configured"
				results[idx] = res
				return
			}

			client, err := geminiweb.DefaultRuntime().GetClient(ctx, a, h.Store)
			if err != nil {
				res.Error = fmt.Sprintf("init client: %v", err)
				results[idx] = res
				return
			}

			quota, err := client.GetFullQuota(ctx, forceRefresh)
			if err != nil {
				res.Error = fmt.Sprintf("get quota: %v", err)
				results[idx] = res
				return
			}

			quota.Identifier = a.Identifier()
			res.Quota = quota
			results[idx] = res
		}(i, acc)
	}

	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{
		"total":    len(results),
		"accounts": results,
	})
}

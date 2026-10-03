package accounts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	dsclient "ds2api/internal/deepseek/client"
	"ds2api/internal/geminiweb"
)

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	page := intFromQuery(r, "page", 1)
	pageSize := intFromQuery(r, "page_size", 10)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	if pageSize > 5000 {
		pageSize = 5000
	}
	allAccounts := h.Store.Snapshot().Accounts
	reverseAccounts(allAccounts)
	// 将已启用且未禁言的账号排在前面，方便管理后台优先看到可用账号。
	sort.SliceStable(allAccounts, func(i, j int) bool {
		ai, aj := allAccounts[i], allAccounts[j]
		activeI := ai.IsEnabled() && !ai.IsMuted()
		activeJ := aj.IsEnabled() && !aj.IsMuted()
		return activeI && !activeJ
	})

	totalCount := len(allAccounts)
	activeCount := 0
	deepseekCount := 0
	geminiCount := 0
	disabledCount := 0
	issuesCount := 0

	for _, acc := range allAccounts {
		provider := acc.AccountProvider()
		if provider == "gemini" {
			geminiCount++
		} else {
			deepseekCount++
		}

		testStatus, _ := h.Store.AccountTestStatus(acc.Identifier())
		isEnabled := acc.IsEnabled()
		isBanned := acc.IsBanned()
		isMuted := acc.IsMuted()
		isFailed := testStatus == "failed"
		isActive := isEnabled && !isBanned && !isMuted && !isFailed
		hasIssue := !isEnabled || isBanned || isMuted || isFailed

		if isActive {
			activeCount++
		}
		if !isEnabled {
			disabledCount++
		}
		if hasIssue {
			issuesCount++
		}
	}

	q := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("q")))
	providerFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	poolTypeFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("pool_type")))
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	proxyFilter := strings.TrimSpace(r.URL.Query().Get("proxy_id"))

	filtered := make([]config.Account, 0, len(allAccounts))
	for _, acc := range allAccounts {
		testStatus, _ := h.Store.AccountTestStatus(acc.Identifier())
		isEnabled := acc.IsEnabled()
		isBanned := acc.IsBanned()
		isMuted := acc.IsMuted()
		isFailed := testStatus == "failed"
		isActive := isEnabled && !isBanned && !isMuted && !isFailed
		hasIssue := !isEnabled || isBanned || isMuted || isFailed

		if providerFilter != "" && providerFilter != "all" {
			if acc.AccountProvider() != providerFilter {
				continue
			}
		}

		if poolTypeFilter != "" && poolTypeFilter != "all" {
			if config.NormalizePoolType(acc.PoolType) != poolTypeFilter {
				continue
			}
		}

		if statusFilter != "" && statusFilter != "all" {
			switch statusFilter {
			case "active":
				if !isActive {
					continue
				}
			case "disabled":
				if isEnabled {
					continue
				}
			case "banned":
				if !isBanned {
					continue
				}
			case "muted":
				if !isMuted {
					continue
				}
			case "failed":
				if !isFailed {
					continue
				}
			case "issues":
				if !hasIssue {
					continue
				}
			}
		}

		if proxyFilter != "" && proxyFilter != "all" {
			if proxyFilter == "direct" {
				if strings.TrimSpace(acc.ProxyID) != "" {
					continue
				}
			} else if acc.ProxyID != proxyFilter {
				continue
			}
		}

		if q != "" {
			id := strings.ToLower(acc.Identifier())
			if !strings.Contains(id, q) &&
				!strings.Contains(strings.ToLower(acc.Name), q) &&
				!strings.Contains(strings.ToLower(acc.Remark), q) &&
				!strings.Contains(strings.ToLower(acc.Email), q) &&
				!strings.Contains(strings.ToLower(acc.Mobile), q) {
				continue
			}
		}
		filtered = append(filtered, acc)
	}

	accounts := filtered
	total := len(accounts)
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	items := make([]map[string]any, 0, end-start)
	for _, acc := range accounts[start:end] {
		testStatus, _ := h.Store.AccountTestStatus(acc.Identifier())
		token := strings.TrimSpace(acc.Token)
		items = append(items, map[string]any{
			"identifier":      acc.Identifier(),
			"name":            acc.Name,
			"remark":          acc.Remark,
			"email":           acc.Email,
			"mobile":          acc.Mobile,
			"proxy_id":        acc.ProxyID,
			"pool_type":       config.NormalizePoolType(acc.PoolType),
			"has_password":    acc.Password != "",
			"has_token":       token != "",
			"token_preview":   maskSecretPreview(token),
			"provider":        acc.AccountProvider(),
			"has_cookies":     strings.TrimSpace(acc.Cookies) != "",
			"cookies_preview": maskSecretPreview(acc.Cookies),
			"test_status":     testStatus,
			"enabled":         acc.IsEnabled(),
			"disabled_reason": acc.DisabledReason,
			"banned":          acc.IsBanned(),
			"muted":           acc.IsMuted(),
			"muted_until":     acc.MutedUntil,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
		"stats": map[string]int{
			"total":    totalCount,
			"active":   activeCount,
			"deepseek": deepseekCount,
			"gemini":   geminiCount,
			"disabled": disabledCount,
			"issues":   issuesCount,
		},
	})
}

func (h *Handler) addAccount(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	acc := toAccount(req)
	if acc.Identifier() == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "需要 identifier (email, mobile 或 name)"})
		return
	}
	if acc.IsGemini() && strings.TrimSpace(acc.Cookies) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Gemini 账号必须配置 cookies"})
		return
	}
	if acc.IsDeepSeek() && strings.TrimSpace(acc.Cookies) != "" {
		if parsed, err := dsclient.ParseDeepSeekSession(acc.Cookies); err == nil {
			if parsed.Token != "" {
				acc.Token = parsed.Token
			}
			if parsed.CookieHeader != "" {
				acc.Cookies = parsed.CookieHeader
			}
		}
	}
	if acc.IsDeepSeek() && strings.TrimSpace(acc.Password) == "" && strings.TrimSpace(acc.Token) == "" && strings.TrimSpace(acc.Cookies) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "DeepSeek 账号需要密码或 Cookie/Session"})
		return
	}
	err := h.Store.Update(func(c *config.Config) error {
		if acc.ProxyID != "" {
			if _, ok := findProxyByID(*c, acc.ProxyID); !ok {
				return fmt.Errorf("代理不存在")
			}
		}
		mobileKey := config.CanonicalMobileKey(acc.Mobile)
		for _, a := range c.Accounts {
			if acc.Email != "" && a.Email == acc.Email {
				return fmt.Errorf("邮箱已存在")
			}
			if mobileKey != "" && config.CanonicalMobileKey(a.Mobile) == mobileKey {
				return fmt.Errorf("手机号已存在")
			}
			if (acc.IsGemini() || acc.IsDeepSeek()) && acc.Name != "" && a.Name == acc.Name && acc.Email == "" && acc.Mobile == "" {
				return fmt.Errorf("账号名称已存在")
			}
		}
		c.Accounts = append(c.Accounts, acc)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_accounts": len(h.Store.Snapshot().Accounts)})
}

func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	if decoded, err := url.PathUnescape(identifier); err == nil {
		identifier = decoded
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	name, nameOK := fieldStringOptional(req, "name")
	remark, remarkOK := fieldStringOptional(req, "remark")
	poolType, poolTypeOK := fieldStringOptional(req, "pool_type")
	cookies, cookiesOK := fieldStringOptional(req, "cookies")
	proxyID, proxyIDOK := fieldStringOptional(req, "proxy_id")

	var canonicalID string
	err := h.Store.Update(func(c *config.Config) error {
		for i, acc := range c.Accounts {
			if !accountMatchesIdentifier(acc, identifier) {
				continue
			}
			canonicalID = acc.Identifier()
			if nameOK {
				c.Accounts[i].Name = name
			}
			if remarkOK {
				c.Accounts[i].Remark = remark
			}
			if poolTypeOK {
				c.Accounts[i].PoolType = config.NormalizePoolType(poolType)
			}
			if cookiesOK && strings.TrimSpace(cookies) != "" {
				cleanCookies := strings.TrimSpace(cookies)
				if c.Accounts[i].IsDeepSeek() {
					if parsed, err := dsclient.ParseDeepSeekSession(cleanCookies); err == nil {
						if parsed.Token != "" {
							c.Accounts[i].Token = parsed.Token
						}
						if parsed.CookieHeader != "" {
							c.Accounts[i].Cookies = parsed.CookieHeader
						} else {
							c.Accounts[i].Cookies = cleanCookies
						}
					} else {
						c.Accounts[i].Cookies = cleanCookies
					}
				} else {
					c.Accounts[i].Cookies = cleanCookies
				}
			}
			if proxyIDOK {
				c.Accounts[i].ProxyID = proxyID
			}
			return nil
		}
		return newRequestError("账号不存在")
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	geminiweb.DefaultRuntime().InvalidateClient(identifier)
	if canonicalID != "" && canonicalID != identifier {
		geminiweb.DefaultRuntime().InvalidateClient(canonicalID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_accounts": len(h.Store.Snapshot().Accounts)})
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	if decoded, err := url.PathUnescape(identifier); err == nil {
		identifier = decoded
	}
	var canonicalID string
	err := h.Store.Update(func(c *config.Config) error {
		idx := -1
		for i, a := range c.Accounts {
			if accountMatchesIdentifier(a, identifier) {
				idx = i
				canonicalID = a.Identifier()
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("账号不存在")
		}
		c.Accounts = append(c.Accounts[:idx], c.Accounts[idx+1:]...)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	geminiweb.DefaultRuntime().InvalidateClient(identifier)
	if canonicalID != "" && canonicalID != identifier {
		geminiweb.DefaultRuntime().InvalidateClient(canonicalID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_accounts": len(h.Store.Snapshot().Accounts)})
}

func (h *Handler) toggleAccountEnabled(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	if decoded, err := url.PathUnescape(identifier); err == nil {
		identifier = decoded
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	enabled, ok := fieldBoolOptional(req, "enabled")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "enabled is required"})
		return
	}

	var current bool
	err := h.Store.Update(func(c *config.Config) error {
		for i, acc := range c.Accounts {
			if !accountMatchesIdentifier(acc, identifier) {
				continue
			}
			c.Accounts[i].Disabled = !enabled
			current = c.Accounts[i].IsEnabled()
			return nil
		}
		return newRequestError("账号不存在")
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "enabled": current})
}

func (h *Handler) batchToggleAccountEnabled(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	enabled, ok := fieldBoolOptional(req, "enabled")
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "enabled is required"})
		return
	}

	var total int
	err := h.Store.Update(func(c *config.Config) error {
		total = len(c.Accounts)
		for i := range c.Accounts {
			c.Accounts[i].Disabled = !enabled
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total": total, "enabled": enabled})
}

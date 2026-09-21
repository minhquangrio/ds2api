package accounts

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

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

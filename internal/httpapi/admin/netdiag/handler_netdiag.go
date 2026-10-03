package netdiag

import (
	"net/http"
	"strings"

	"ds2api/internal/config"
	adminshared "ds2api/internal/httpapi/admin/shared"
	"ds2api/internal/netdiag"
)

type Handler struct {
	Store adminshared.ConfigStore
}

func (h *Handler) detectNetwork(w http.ResponseWriter, r *http.Request) {
	var targetProxy *config.Proxy

	if h.Store != nil {
		proxyID := strings.TrimSpace(r.URL.Query().Get("proxy_id"))
		if proxyID != "" {
			cfg := h.Store.Snapshot()
			for _, p := range cfg.Proxies {
				if p.ID == proxyID {
					cp := config.NormalizeProxy(p)
					targetProxy = &cp
					break
				}
			}
		}
	}

	report := netdiag.CollectReport(r.Context(), targetProxy)
	adminshared.WriteJSON(w, http.StatusOK, report)
}

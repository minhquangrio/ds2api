package netdiag

import (
	"net/http"
	"strings"

	"ds2api/internal/config"
	adminshared "ds2api/internal/httpapi/admin/shared"
	"ds2api/internal/netdiag"
	"ds2api/internal/proxyrotate"
)

type Handler struct {
	Store adminshared.ConfigStore
}

func (h *Handler) detectNetwork(w http.ResponseWriter, r *http.Request) {
	var targetProxy *config.Proxy

	if h.Store != nil {
		proxyID := strings.TrimSpace(r.URL.Query().Get("proxy_id"))
		if proxyID != "" {
			// Rotating entries are resolved first so the report describes the
			// exit node the runtime would actually use.
			resolved, found, err := proxyrotate.ResolveProxyID(r.Context(), h.Store.Snapshot(), proxyID)
			if err != nil {
				adminshared.WriteJSON(w, http.StatusBadGateway, map[string]any{"detail": err.Error()})
				return
			}
			if found {
				cp := config.NormalizeProxy(resolved)
				targetProxy = &cp
			}
		}
	}

	report := netdiag.CollectReport(r.Context(), targetProxy)
	adminshared.WriteJSON(w, http.StatusOK, report)
}

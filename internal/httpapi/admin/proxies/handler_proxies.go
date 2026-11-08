package proxies

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	dsclient "ds2api/internal/deepseek/client"
	adminshared "ds2api/internal/httpapi/admin/shared"
	"ds2api/internal/proxyrotate"
)

var proxyConnectivityTester = func(ctx context.Context, proxy config.Proxy) map[string]any {
	return dsclient.TestProxyConnectivity(ctx, proxy)
}

func validateProxyMutation(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}
	if err := config.ValidateProxyConfig(cfg.Proxies); err != nil {
		return err
	}
	return config.ValidateAccountProxyReferences(cfg.Accounts, cfg.Proxies)
}

func proxyResponse(proxy config.Proxy) map[string]any {
	return adminshared.ProxyView(proxy)
}

func (h *Handler) listProxies(w http.ResponseWriter, _ *http.Request) {
	items := adminshared.ProxyViews(h.Store.Snapshot().Proxies)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) addProxy(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	proxy := toProxy(req)
	err := h.Store.Update(func(c *config.Config) error {
		c.Proxies = append(c.Proxies, proxy)
		return validateProxyMutation(c)
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "proxy": proxyResponse(proxy)})
}

// mergeProxyUpdate keeps stored values that a partial update did not repeat.
// Password and rotating key follow the same "empty means keep" rule, and a body
// that never mentions rotation keeps the entry in its previous mode.
func mergeProxyUpdate(proxy, existing config.Proxy, req map[string]any) config.Proxy {
	proxy = config.NormalizeProxy(proxy)
	existing = config.NormalizeProxy(existing)

	if !adminshared.HasRotatePayload(req) {
		proxy.Rotate = existing.Rotate
		if proxy.Type == "" {
			proxy.Type = existing.Type
		}
		if proxy.Host == "" {
			proxy.Host = existing.Host
		}
		if proxy.Port == 0 {
			proxy.Port = existing.Port
		}
		if proxy.Username == "" {
			proxy.Username = existing.Username
		}
		if proxy.Name == "" {
			proxy.Name = existing.Name
		}
	}
	// Re-normalize so a rotating entry drops the static fields it cannot use.
	proxy = config.NormalizeProxy(proxy)

	if proxy.Rotate != nil {
		// Key and endpoint are the two values a partial edit must never wipe:
		// an empty one means "keep what is stored", like the proxy password.
		if existing.Rotate != nil {
			if strings.TrimSpace(proxy.Rotate.Key) == "" {
				proxy.Rotate.Key = existing.Rotate.Key
			}
			if strings.TrimSpace(proxy.Rotate.Endpoint) == "" {
				proxy.Rotate.Endpoint = existing.Rotate.Endpoint
			}
		}
		return proxy
	}
	if proxy.Password == "" {
		proxy.Password = existing.Password
	}
	return proxy
}

func (h *Handler) updateProxy(w http.ResponseWriter, r *http.Request) {
	proxyID := chi.URLParam(r, "proxyID")
	if decoded, err := url.PathUnescape(proxyID); err == nil {
		proxyID = decoded
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	incoming := toProxy(req)
	incoming.ID = strings.TrimSpace(proxyID)

	var merged config.Proxy
	err := h.Store.Update(func(c *config.Config) error {
		idx := -1
		for i, existing := range c.Proxies {
			if config.NormalizeProxy(existing).ID == incoming.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return newRequestError("代理不存在")
		}
		previous := config.NormalizeProxy(c.Proxies[idx])
		merged = mergeProxyUpdate(incoming, previous, req)
		if merged.IsRotating() != previous.IsRotating() {
			// Mode switch: a lease from the previous mode must not linger.
			proxyrotate.Global().Forget(merged.ID)
		}
		c.Proxies[idx] = merged
		return validateProxyMutation(c)
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	// The lease is keyed by a fingerprint of the rotate config, so an edit that
	// really changes the source renews on the next request by itself. Forcing a
	// refresh here would burn a provider call on every rename.
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "proxy": proxyResponse(merged)})
}

func (h *Handler) deleteProxy(w http.ResponseWriter, r *http.Request) {
	proxyID := chi.URLParam(r, "proxyID")
	if decoded, err := url.PathUnescape(proxyID); err == nil {
		proxyID = decoded
	}
	err := h.Store.Update(func(c *config.Config) error {
		idx := -1
		for i, existing := range c.Proxies {
			existing = config.NormalizeProxy(existing)
			if existing.ID == strings.TrimSpace(proxyID) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return newRequestError("代理不存在")
		}
		c.Proxies = append(c.Proxies[:idx], c.Proxies[idx+1:]...)
		for i := range c.Accounts {
			if strings.TrimSpace(c.Accounts[i].ProxyID) == strings.TrimSpace(proxyID) {
				c.Accounts[i].ProxyID = ""
			}
		}
		return validateProxyMutation(c)
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	proxyrotate.Global().Forget(strings.TrimSpace(proxyID))
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) testProxy(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	proxyID := fieldString(req, "proxy_id")

	var proxy config.Proxy
	if proxyID != "" {
		var ok bool
		proxy, ok = findProxyByID(h.Store.Snapshot(), proxyID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"detail": "代理不存在"})
			return
		}
	} else {
		proxy = toProxy(req)
	}

	result := proxyConnectivityTester(r.Context(), proxy)
	writeJSON(w, http.StatusOK, result)
}

// refreshProxy forces a rotating entry to fetch a new exit node right now,
// which is what the WebUI "lấy IP mới" button calls.
func (h *Handler) refreshProxy(w http.ResponseWriter, r *http.Request) {
	proxyID := chi.URLParam(r, "proxyID")
	if decoded, err := url.PathUnescape(proxyID); err == nil {
		proxyID = decoded
	}
	proxy, ok := findProxyByID(h.Store.Snapshot(), strings.TrimSpace(proxyID))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "代理不存在"})
		return
	}
	if !proxy.IsRotating() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "该代理未启用轮换模式"})
		return
	}

	resolved, err := proxyrotate.ForceRefresh(r.Context(), proxy)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"success":  false,
			"proxy_id": config.NormalizeProxy(proxy).ID,
			"detail":   err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"proxy":    proxyResponse(proxy),
		"endpoint": resolved.Host,
		"port":     resolved.Port,
		"type":     resolved.Type,
	})
}

func (h *Handler) updateAccountProxy(w http.ResponseWriter, r *http.Request) {
	identifier := chi.URLParam(r, "identifier")
	if decoded, err := url.PathUnescape(identifier); err == nil {
		identifier = decoded
	}
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	proxyID := fieldString(req, "proxy_id")

	err := h.Store.Update(func(c *config.Config) error {
		if proxyID != "" {
			if _, ok := findProxyByID(*c, proxyID); !ok {
				return newRequestError("代理不存在")
			}
		}
		for i, acc := range c.Accounts {
			if !accountMatchesIdentifier(acc, identifier) {
				continue
			}
			c.Accounts[i].ProxyID = proxyID
			return validateProxyMutation(c)
		}
		return newRequestError("账号不存在")
	})
	if err != nil {
		if detail, ok := requestErrorDetail(err); ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": detail})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "proxy_id": proxyID})
}

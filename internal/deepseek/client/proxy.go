package client

import (
	"context"
	dsprotocol "ds2api/internal/deepseek/protocol"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/proxy"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	trans "ds2api/internal/deepseek/transport"
	"ds2api/internal/proxyrotate"
)

type requestClients struct {
	regular   trans.Doer
	stream    trans.Doer
	fallback  trans.Doer
	fallbackS trans.Doer
}

type cachedProxyClients struct {
	proxy    config.Proxy
	clients  requestClients
	usage    *proxyUsage
	retiring bool
	// retiredAt starts the grace window during which requests that began
	// before the rotation may still use this bundle.
	retiredAt time.Time
}

type idleConnectionCloser interface {
	CloseIdleConnections()
}

func closeRequestClients(bundle requestClients) {
	for _, doer := range []trans.Doer{bundle.regular, bundle.stream, bundle.fallback, bundle.fallbackS} {
		if closer, ok := doer.(idleConnectionCloser); ok {
			closer.CloseIdleConnections()
		}
	}
}

type hostLookupFunc func(ctx context.Context, network, host string) ([]string, error)

var proxyConnectivityTestURL = "https://chat.deepseek.com/"

var defaultHostLookup hostLookupFunc = func(ctx context.Context, _ string, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// proxyDialAddress returns the address to hand to the proxy dialer.
//
// The hostname is passed through untouched so the SOCKS5 server resolves it at
// the exit node (SOCKS5 ATYP=3, which golang.org/x/net/proxy speaks natively).
// Resolving locally first would leak DNS queries for chat.deepseek.com from the
// host running this proxy, and could pin an account's traffic to an edge IP
// that does not match the geography of its exit node.
func proxyDialAddress(_ context.Context, _, address string, _ hostLookupFunc) (string, error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return "", err
	}
	return address, nil
}

func proxyCacheKey(proxyCfg config.Proxy) string {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	return strings.Join([]string{
		proxyCfg.ID,
		proxyCfg.Type,
		strings.ToLower(proxyCfg.Host),
		strconv.Itoa(proxyCfg.Port),
		proxyCfg.Username,
		proxyCfg.Password,
	}, "|")
}

func proxyDialContext(proxyCfg config.Proxy) (trans.DialContextFunc, error) {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	scheme := strings.ToLower(strings.TrimSpace(proxyCfg.Type))
	if scheme == "http" || scheme == "https" {
		return nil, nil
	}
	var authCfg *proxy.Auth
	if proxyCfg.Username != "" || proxyCfg.Password != "" {
		authCfg = &proxy.Auth{User: proxyCfg.Username, Password: proxyCfg.Password}
	}
	forward := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort(proxyCfg.Host, strconv.Itoa(proxyCfg.Port)), authCfg, forward)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		target, err := proxyDialAddress(ctx, proxyCfg.Type, address, defaultHostLookup)
		if err != nil {
			return nil, err
		}
		if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
			return ctxDialer.DialContext(ctx, network, target)
		}
		return dialer.Dial(network, target)
	}, nil
}

func httpCloakProxyURL(proxyCfg config.Proxy) string {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	scheme := strings.ToLower(strings.TrimSpace(proxyCfg.Type))
	if scheme == "socks5h" {
		scheme = "socks5"
	}
	if scheme == "" {
		scheme = "socks5"
	}
	u := &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(proxyCfg.Host, strconv.Itoa(proxyCfg.Port)),
	}
	if proxyCfg.Username != "" {
		u.User = url.UserPassword(proxyCfg.Username, proxyCfg.Password)
	}
	return u.String()
}

func (c *Client) defaultRequestClients() requestClients {
	return c.decorate(requestClients{
		regular:   c.regular,
		stream:    c.stream,
		fallback:  c.fallback,
		fallbackS: c.fallbackS,
	})
}

// decorate layers per-account cookie replay and response decompression over a
// bundle. Both have to apply on every path, including the std-transport
// fallbacks, so responses look identical to callers regardless of which
// transport served them.
func (c *Client) decorate(bundle requestClients) requestClients {
	return requestClients{
		regular:   newWireDoer(bundle.regular, c.cookies),
		stream:    newWireDoer(bundle.stream, c.cookies),
		fallback:  newWireDoer(bundle.fallback, c.cookies),
		fallbackS: newWireDoer(bundle.fallbackS, c.cookies),
	}
}

func (c *Client) resolveProxyForAccount(acc config.Account) (config.Proxy, bool) {
	if c == nil || c.Store == nil {
		return config.Proxy{}, false
	}
	proxyID := strings.TrimSpace(acc.ProxyID)
	if proxyID == "" {
		return config.Proxy{}, false
	}
	snap := c.Store.Snapshot()
	for _, proxyCfg := range snap.Proxies {
		proxyCfg = config.NormalizeProxy(proxyCfg)
		if proxyCfg.ID == proxyID {
			return proxyCfg, true
		}
	}
	return config.Proxy{}, false
}

func (c *Client) requestClientsFromContext(ctx context.Context) requestClients {
	if a, ok := auth.FromContext(ctx); ok {
		return c.requestClientsForAccount(ctx, a.Account)
	}
	return c.defaultRequestClients()
}

func (c *Client) requestClientsForAuth(ctx context.Context, a *auth.RequestAuth) requestClients {
	if a != nil {
		return c.requestClientsForAccount(ctx, a.Account)
	}
	return c.requestClientsFromContext(ctx)
}

func (c *Client) requestClientsForAccount(ctx context.Context, acc config.Account) requestClients {
	proxyCfg, ok := c.resolveProxyForAccount(acc)
	if !ok {
		return c.defaultRequestClients()
	}

	// A rotating entry hands out a different exit node over time, so the
	// concrete endpoint has to be resolved before the bundle lookup.
	resolved, err := proxyrotate.Resolve(ctx, proxyCfg)
	if err != nil {
		config.Logger.Warn("[proxy] rotating endpoint unavailable", "proxy_id", proxyCfg.ID, "error", err)
		return c.failedProxyClients(proxyCfg.ID, err)
	}

	bundle, err := c.proxyClientsFor(resolved)
	if err != nil {
		if proxyCfg.IsRotating() {
			// The node was reported alive but cannot be dialled: fail rather
			// than silently sending this account direct.
			config.Logger.Warn("[proxy] rotating endpoint dialer failed", "proxy_id", proxyCfg.ID, "error", err)
			return c.failedProxyClients(proxyCfg.ID, err)
		}
		config.Logger.Warn("[proxy] build dialer failed", "proxy_id", proxyCfg.ID, "error", err)
		return c.defaultRequestClients()
	}
	return bundle
}

// proxyClientsFor returns the transport bundle for one concrete proxy endpoint,
// building it on first use and retiring the previous endpoint of the same
// configured entry when a rotation changed the address.
func (c *Client) proxyClientsFor(proxyCfg config.Proxy) (requestClients, error) {
	proxyCfg = config.NormalizeProxy(proxyCfg)
	key := proxyCacheKey(proxyCfg)

	c.proxyClientsMu.RLock()
	cached, ok := c.proxyClients[key]
	c.proxyClientsMu.RUnlock()
	if ok && !cached.retiring && proxyCacheKey(cached.proxy) == key {
		return cached.clients, nil
	}

	dialContext, err := proxyDialContext(proxyCfg)
	if err != nil {
		return requestClients{}, err
	}

	proxyURL := httpCloakProxyURL(proxyCfg)
	usage := &proxyUsage{}
	bundle := trackRequestClients(c.decorate(requestClients{
		regular:   trans.NewWithProxy(60*time.Second, proxyURL),
		stream:    trans.NewWithProxy(0, proxyURL),
		fallback:  trans.NewFallbackClientWithProxy(60*time.Second, proxyURL, dialContext),
		fallbackS: trans.NewFallbackClientWithProxy(0, proxyURL, dialContext),
	}), usage)

	c.proxyClientsMu.Lock()
	if c.proxyClients == nil {
		c.proxyClients = make(map[string]cachedProxyClients)
	}
	if current, exists := c.proxyClients[key]; exists && proxyCacheKey(current.proxy) == key {
		// The same endpoint is requested again (for example a rotation moved
		// back to it); bring it out of retirement instead of tracking two
		// bundles for one address.
		current.retiring = false
		c.proxyClients[key] = current
		c.proxyClientsMu.Unlock()
		closeRequestClients(bundle)
		return current.clients, nil
	}
	c.proxyClients[key] = cachedProxyClients{proxy: proxyCfg, clients: bundle, usage: usage}
	closeLater := c.retireProxyClients(proxyCfg.ID, key)
	closeLater = append(closeLater, c.sweepRetiredProxyClients(now())...)
	c.proxyClientsMu.Unlock()

	for _, retired := range closeLater {
		closeRequestClients(retired)
	}
	return bundle, nil
}

func applyProxyConnectivityHeaders(req *http.Request) {
	if req == nil {
		return
	}
	for key, value := range dsprotocol.BaseHeaders {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}

func proxyConnectivityStatus(statusCode int) (bool, string) {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return true, fmt.Sprintf("代理可达，目标返回 HTTP %d", statusCode)
	case statusCode >= 300 && statusCode < 500:
		return true, fmt.Sprintf("代理可达，但目标返回 HTTP %d（可能是风控或挑战）", statusCode)
	default:
		return false, fmt.Sprintf("目标返回 HTTP %d", statusCode)
	}
}

func TestProxyConnectivity(ctx context.Context, proxyCfg config.Proxy) map[string]any {
	start := time.Now()
	proxyCfg = config.NormalizeProxy(proxyCfg)
	result := map[string]any{
		"success":       false,
		"proxy_id":      proxyCfg.ID,
		"proxy_type":    proxyCfg.Type,
		"response_time": 0,
	}

	if proxyCfg.IsRotating() {
		// The connectivity check must exercise the node the runtime would
		// actually use, so resolve the rotating entry first.
		resolved, err := proxyrotate.Resolve(ctx, proxyCfg)
		if err != nil {
			result["message"] = "无法获取轮换代理节点: " + err.Error()
			result["response_time"] = int(time.Since(start).Milliseconds())
			return result
		}
		result["rotated_endpoint"] = net.JoinHostPort(resolved.Host, strconv.Itoa(resolved.Port))
		result["proxy_type"] = resolved.Type
		proxyCfg = config.NormalizeProxy(resolved)
	}

	if err := config.ValidateProxyConfig([]config.Proxy{proxyCfg}); err != nil {
		result["message"] = "代理配置无效: " + err.Error()
		return result
	}
	dialContext, err := proxyDialContext(proxyCfg)
	if err != nil {
		result["message"] = "代理拨号器初始化失败: " + err.Error()
		return result
	}

	proxyURL := httpCloakProxyURL(proxyCfg)
	client := trans.NewFallbackClientWithProxy(15*time.Second, proxyURL, dialContext)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyConnectivityTestURL, nil)
	if err != nil {
		result["message"] = err.Error()
		return result
	}
	applyProxyConnectivityHeaders(req)

	resp, err := client.Do(req)
	result["response_time"] = int(time.Since(start).Milliseconds())
	if err != nil {
		result["message"] = err.Error()
		return result
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			config.Logger.Warn("[proxy] close response body failed", "proxy_id", proxyCfg.ID, "error", closeErr)
		}
	}()

	result["status_code"] = resp.StatusCode
	result["success"], result["message"] = proxyConnectivityStatus(resp.StatusCode)
	return result
}

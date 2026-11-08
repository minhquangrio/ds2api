package proxyrotate

import (
	"context"
	"sync"

	"ds2api/internal/config"
)

var (
	globalOnce    sync.Once
	globalManager *Manager
)

// Global is the process-wide lease manager shared by every provider path
// (DeepSeek, Codex, Gemini Web, Muse) so one rotating key is fetched once.
func Global() *Manager {
	globalOnce.Do(func() { globalManager = NewManager(nil) })
	return globalManager
}

// Resolve resolves one configured proxy entry through the shared manager.
func Resolve(ctx context.Context, proxyCfg config.Proxy) (config.Proxy, error) {
	return Global().Resolve(ctx, proxyCfg)
}

// ResolveProxyID looks a configured entry up in cfg and resolves it. The
// boolean reports whether the id exists at all; an error means the rotating
// source could not provide a usable node.
func ResolveProxyID(ctx context.Context, cfg config.Config, proxyID string) (config.Proxy, bool, error) {
	return Global().ResolveProxyID(ctx, cfg, proxyID)
}

// ForceRefresh drops the lease of one rotating entry and fetches a new node.
func ForceRefresh(ctx context.Context, proxyCfg config.Proxy) (config.Proxy, error) {
	return Global().ForceRefresh(ctx, proxyCfg)
}

// Invalidate marks a lease as due without fetching, so the next request renews
// it. Used when a transport reports that the current node died.
func Invalidate(proxyID string) { Global().Invalidate(proxyID) }

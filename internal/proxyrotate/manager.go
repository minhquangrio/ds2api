package proxyrotate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"ds2api/internal/config"
)

const (
	initialBackoff = 5 * time.Second
	maxBackoff     = 60 * time.Second
)

// Manager caches one exit node per rotating proxy entry and renews it shortly
// before the provider says it dies.
type Manager struct {
	mu           sync.Mutex
	httpClient   *http.Client
	fetchTimeout time.Duration
	now          func() time.Time
	leases       map[string]*leaseState
}

type leaseState struct {
	// fetchMu serializes provider calls for one proxy entry so a burst of
	// concurrent requests renews the lease once instead of burning API quota.
	fetchMu sync.Mutex

	// fingerprint of the rotate config the lease was fetched under.
	fingerprint string

	// Everything below is guarded by Manager.mu.
	current    config.Proxy
	expiresAt  time.Time
	renewAt    time.Time
	fetchedAt  time.Time
	network    string
	location   string
	fetchCount int64
	lastErr    error
	lastErrAt  time.Time
	backoff    time.Duration
}

// NewManager builds a manager with an optional HTTP client (nil = direct,
// environment-aware client).
func NewManager(httpClient *http.Client) *Manager {
	if httpClient == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = http.ProxyFromEnvironment
		httpClient = &http.Client{Timeout: fetchTimeout, Transport: transport}
	}
	return &Manager{
		httpClient:   httpClient,
		fetchTimeout: fetchTimeout,
		now:          time.Now,
		leases:       make(map[string]*leaseState),
	}
}

// SetClock overrides the time source; test-only.
func (m *Manager) SetClock(now func() time.Time) {
	if m == nil || now == nil {
		return
	}
	m.mu.Lock()
	m.now = now
	m.mu.Unlock()
}

func (m *Manager) stateFor(proxyID string) *leaseState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leases == nil {
		m.leases = make(map[string]*leaseState)
	}
	state, ok := m.leases[proxyID]
	if !ok {
		state = &leaseState{}
		m.leases[proxyID] = state
	}
	return state
}

func rotateFingerprint(rotate config.RotateProxyConfig) string {
	sum := sha1.Sum([]byte(strings.Join([]string{
		rotate.Provider,
		strings.ToLower(rotate.Endpoint),
		rotate.Key,
		rotate.Protocol,
		strings.ToLower(rotate.Network),
		strings.ToLower(rotate.Province),
		rotate.Whitelist,
		fmt.Sprintf("%d", rotate.TTLSeconds),
		fmt.Sprintf("%d", rotate.MinRemainingSeconds),
	}, "|")))
	return hex.EncodeToString(sum[:8])
}

// Resolve returns a concrete proxy usable by transports. Static entries pass
// through untouched; rotating entries reuse their lease until it is about to
// expire and then fetch a new node. It never falls back to a direct connection,
// because an account pinned to a proxy must not leak the host IP.
func (m *Manager) Resolve(ctx context.Context, proxyCfg config.Proxy) (config.Proxy, error) {
	normalized := config.NormalizeProxy(proxyCfg)
	if normalized.Rotate == nil {
		return normalized, nil
	}
	if err := config.ValidateProxyConfig([]config.Proxy{normalized}); err != nil {
		return config.Proxy{}, err
	}

	state := m.stateFor(normalized.ID)
	state.fetchMu.Lock()
	defer state.fetchMu.Unlock()

	fingerprint := rotateFingerprint(*normalized.Rotate)
	if candidate, ok := m.reusable(state, fingerprint); ok {
		return candidate, nil
	}
	if blocked, cachedErr := m.retryBlocked(state); blocked {
		if candidate, ok := m.graceUsable(state); ok {
			return candidate, nil
		}
		if cachedErr != nil {
			return config.Proxy{}, cachedErr
		}
	}

	resolved, err := m.fetch(ctx, normalized)
	m.mu.Lock()
	state.fetchCount++
	if err != nil {
		state.lastErr = err
		state.lastErrAt = m.now()
		state.backoff = nextBackoff(state.backoff)
		stale, hasStale := m.graceUsableLocked(state)
		m.mu.Unlock()
		if hasStale {
			config.Logger.Warn("[proxyrotate] renew failed, keeping current node",
				"proxy_id", normalized.ID, "endpoint", fmt.Sprintf("%s:%d", stale.Host, stale.Port), "error", err)
			return stale, nil
		}
		config.Logger.Warn("[proxyrotate] renew failed and no usable node",
			"proxy_id", normalized.ID, "error", err)
		return config.Proxy{}, err
	}

	now := m.now()
	rotate := *normalized.Rotate
	state.fingerprint = fingerprint
	state.current = resolved.proxy
	state.fetchedAt = now
	state.expiresAt = resolved.expiresAt
	state.network = resolved.network
	state.location = resolved.location
	remaining := int(resolved.expiresAt.Sub(now).Seconds())
	minRemaining := rotate.MinRemainingSeconds
	if minRemaining > remaining/2 {
		minRemaining = remaining / 2
	}
	if minRemaining < 1 {
		minRemaining = 1
	}
	state.renewAt = resolved.expiresAt.Add(-time.Duration(minRemaining) * time.Second)
	if state.renewAt.Before(now) {
		state.renewAt = now
	}
	state.lastErr = nil
	state.lastErrAt = time.Time{}
	state.backoff = 0
	m.mu.Unlock()

	config.Logger.Info("[proxyrotate] rotated exit node",
		"proxy_id", normalized.ID,
		"endpoint", fmt.Sprintf("%s:%d", resolved.proxy.Host, resolved.proxy.Port),
		"type", resolved.proxy.Type,
		"network", resolved.network,
		"location", resolved.location,
		"expires_in", int(resolved.expiresAt.Sub(now).Seconds()))
	return resolved.proxy, nil
}

// ForceRefresh drops the lease and immediately asks the provider for a new node.
func (m *Manager) ForceRefresh(ctx context.Context, proxyCfg config.Proxy) (config.Proxy, error) {
	normalized := config.NormalizeProxy(proxyCfg)
	if normalized.Rotate == nil {
		return normalized, nil
	}
	m.Invalidate(normalized.ID)
	return m.Resolve(ctx, normalized)
}

// Invalidate marks a lease as due so the next Resolve renews it. The current
// node stays usable until its hard expiry, so in-flight traffic is not killed.
func (m *Manager) Invalidate(proxyID string) {
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return
	}
	state := m.stateFor(proxyID)
	state.fetchMu.Lock()
	defer state.fetchMu.Unlock()
	m.mu.Lock()
	state.renewAt = time.Time{}
	m.mu.Unlock()
}

func (m *Manager) reusable(state *leaseState, fingerprint string) (config.Proxy, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state.current.Host == "" || state.fingerprint != fingerprint {
		return config.Proxy{}, false
	}
	if m.now().Before(state.renewAt) {
		return state.current, true
	}
	return config.Proxy{}, false
}

func (m *Manager) graceUsable(state *leaseState) (config.Proxy, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.graceUsableLocked(state)
}

// graceUsableLocked returns the node while the provider says it is still alive.
func (m *Manager) graceUsableLocked(state *leaseState) (config.Proxy, bool) {
	if state.current.Host == "" {
		return config.Proxy{}, false
	}
	if m.now().Before(state.expiresAt) {
		return state.current, true
	}
	return config.Proxy{}, false
}

func (m *Manager) retryBlocked(state *leaseState) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state.lastErr == nil || state.backoff <= 0 {
		return false, nil
	}
	if m.now().Before(state.lastErrAt.Add(state.backoff)) {
		return true, state.lastErr
	}
	return false, nil
}

func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return initialBackoff
	}
	doubled := current * 2
	if doubled > maxBackoff {
		return maxBackoff
	}
	return doubled
}

// Status reports the live lease for one proxy entry.
func (m *Manager) Status(proxyID string) (config.RotateLease, bool) {
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return config.RotateLease{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.leases[proxyID]
	if !ok {
		return config.RotateLease{}, false
	}
	lease := m.leaseLocked(state)
	return lease, lease.Host != "" || lease.LastError != ""
}

// Statuses reports every known proxy entry, keyed by proxy id.
func (m *Manager) Statuses() map[string]config.RotateLease {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]config.RotateLease, len(m.leases))
	for id, state := range m.leases {
		lease := m.leaseLocked(state)
		if lease.Host == "" && lease.LastError == "" {
			continue
		}
		out[id] = lease
	}
	return out
}

// leaseLocked builds the public lease view. Callers must hold m.mu.
func (m *Manager) leaseLocked(state *leaseState) config.RotateLease {
	now := m.now()
	lease := config.RotateLease{
		Host:       state.current.Host,
		Port:       state.current.Port,
		Type:       state.current.Type,
		Network:    state.network,
		Location:   state.location,
		FetchCount: state.fetchCount,
	}
	if !state.expiresAt.IsZero() {
		lease.ExpiresAtUnix = state.expiresAt.Unix()
		if remaining := int64(state.expiresAt.Sub(now).Seconds()); remaining > 0 {
			lease.RemainingSecond = remaining
		}
	}
	if !state.fetchedAt.IsZero() {
		lease.FetchedAtUnix = state.fetchedAt.Unix()
	}
	if state.lastErr != nil {
		lease.LastError = state.lastErr.Error()
	}
	return lease
}

// Forget releases bookkeeping for a removed proxy entry.
func (m *Manager) Forget(proxyID string) {
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return
	}
	m.mu.Lock()
	delete(m.leases, proxyID)
	m.mu.Unlock()
}

// FindProxy locates a configured proxy entry by id.
func FindProxy(cfg config.Config, proxyID string) (config.Proxy, bool) {
	id := strings.TrimSpace(proxyID)
	if id == "" {
		return config.Proxy{}, false
	}
	for _, proxy := range cfg.Proxies {
		proxy = config.NormalizeProxy(proxy)
		if proxy.ID == id {
			return proxy, true
		}
	}
	return config.Proxy{}, false
}

// ResolveProxyID resolves a configured entry (static or rotating) into a
// concrete endpoint. A missing entry is reported as (Proxy{}, false, nil) so
// callers keep their current "no proxy" behaviour.
func (m *Manager) ResolveProxyID(ctx context.Context, cfg config.Config, proxyID string) (config.Proxy, bool, error) {
	proxyCfg, ok := FindProxy(cfg, proxyID)
	if !ok {
		return config.Proxy{}, false, nil
	}
	resolved, err := m.Resolve(ctx, proxyCfg)
	if err != nil {
		return config.Proxy{}, true, err
	}
	return resolved, true, nil
}

// ResolveAccount resolves the proxy bound to an account, following rotation.
func ResolveAccount(ctx context.Context, store *config.Store, acc config.Account) (config.Proxy, bool, error) {
	if store == nil {
		return config.Proxy{}, false, errors.New("proxyrotate: config store is nil")
	}
	proxyID := strings.TrimSpace(acc.ProxyID)
	if proxyID == "" {
		return config.Proxy{}, false, nil
	}
	return Global().ResolveProxyID(ctx, store.Snapshot(), proxyID)
}

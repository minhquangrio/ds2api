package config

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// Rotating proxy ("proxy xoay") sources. A Proxy that carries a Rotate config
// has no fixed Host/Port: the runtime asks the provider API for a fresh exit
// node and keeps using it until it is about to expire.
const (
	// ProxyRotateProviderProxyXoay is the proxyxoay.shop get endpoint.
	ProxyRotateProviderProxyXoay = "proxyxoay"
	// ProxyRotateProviderBestProxy is the bestproxy.vn proxyxoay get endpoint.
	ProxyRotateProviderBestProxy = "bestproxy"
	// ProxyRotateProviderCustom requires an explicit Endpoint.
	ProxyRotateProviderCustom = "custom"

	// DefaultRotateTTLSeconds is the lease length used when the provider does
	// not report how long the returned proxy stays alive.
	DefaultRotateTTLSeconds = 300
	// DefaultRotateMinRemainingSeconds is how early a lease is renewed so a
	// request never starts on an endpoint that dies mid-flight.
	DefaultRotateMinRemainingSeconds = 60
	// DefaultRotateMaxTTLSeconds caps a provider-reported lifetime so one exit
	// IP is not pinned forever by a bogus number.
	DefaultRotateMaxTTLSeconds = 86400
)

// RotateProxyConfig describes where to fetch rotating exit nodes from.
type RotateProxyConfig struct {
	// Provider selects a preset endpoint: proxyxoay | bestproxy | custom.
	Provider string `json:"provider,omitempty"`
	// Endpoint overrides the provider preset. Must be an http(s) URL.
	Endpoint string `json:"endpoint,omitempty"`
	// Key is the "keyxoay" secret issued when the rotating key is purchased.
	Key string `json:"key,omitempty"`
	// Network maps to the provider's `nhamang` parameter (random, fpt, ...).
	Network string `json:"network,omitempty"`
	// Province maps to `tinhthanh`; "0" means random.
	Province string `json:"province,omitempty"`
	// Protocol picks which returned endpoint to use: http | socks5.
	Protocol string `json:"protocol,omitempty"`
	// Whitelist maps to `whitelist`: the IPv4 allowed to use the node.
	Whitelist string `json:"whitelist,omitempty"`
	// TTLSeconds is the fallback lease when the API omits a lifetime.
	TTLSeconds int `json:"ttl_seconds,omitempty"`
	// MinRemainingSeconds is the refresh threshold before lease expiry.
	MinRemainingSeconds int `json:"min_remaining_seconds,omitempty"`
}

// NormalizeRotateProxy trims fields and applies defaults. A nil input stays nil
// so a static proxy never grows an empty rotate block.
func NormalizeRotateProxy(r *RotateProxyConfig) *RotateProxyConfig {
	if r == nil {
		return nil
	}
	out := *r
	out.Provider = strings.ToLower(strings.TrimSpace(out.Provider))
	if out.Provider == "" {
		out.Provider = ProxyRotateProviderProxyXoay
	}
	out.Endpoint = strings.TrimSpace(out.Endpoint)
	out.Key = strings.TrimSpace(out.Key)
	out.Network = strings.TrimSpace(out.Network)
	if out.Network == "" {
		out.Network = "random"
	}
	out.Province = strings.TrimSpace(out.Province)
	if out.Province == "" {
		out.Province = "0"
	}
	out.Protocol = strings.ToLower(strings.TrimSpace(out.Protocol))
	switch out.Protocol {
	case "socks5", "socks5h":
		out.Protocol = "socks5"
	case "https":
		out.Protocol = "http"
	default:
		out.Protocol = "http"
	}
	out.Whitelist = strings.TrimSpace(out.Whitelist)
	if out.TTLSeconds <= 0 {
		out.TTLSeconds = DefaultRotateTTLSeconds
	}
	if out.MinRemainingSeconds <= 0 {
		out.MinRemainingSeconds = DefaultRotateMinRemainingSeconds
	}
	// A renewal window larger than the lease would make every request refresh.
	if out.MinRemainingSeconds >= out.TTLSeconds {
		out.MinRemainingSeconds = out.TTLSeconds - 1
		if out.MinRemainingSeconds < 1 {
			out.MinRemainingSeconds = 1
		}
	}
	return &out
}

// RotateEndpointHost is the URL host of the fetch endpoint, used for logging.
func RotateEndpointHost(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if idx := strings.Index(endpoint, "://"); idx >= 0 {
		rest := endpoint[idx+3:]
		if cut := strings.IndexAny(rest, "/?"); cut >= 0 {
			return rest[:cut]
		}
		return rest
	}
	return endpoint
}

// rotateStableProxyID hashes the rotate identity so two different keys or
// networks get distinct proxy ids while a rename keeps the same id.
func rotateStableProxyID(r RotateProxyConfig) string {
	key := strings.Join([]string{
		"rotate",
		r.Provider,
		strings.ToLower(r.Endpoint),
		r.Key,
		r.Protocol,
		r.Network,
		r.Province,
	}, "|")
	sum := sha1.Sum([]byte(key))
	return "proxy_" + hex.EncodeToString(sum[:6])
}

// RotateLease is the runtime view of one fetched exit node. It is exposed by
// the admin API so the WebUI can show the live IP and its countdown.
type RotateLease struct {
	Host            string `json:"host,omitempty"`
	Port            int    `json:"port,omitempty"`
	Type            string `json:"type,omitempty"`
	ExpiresAtUnix   int64  `json:"expires_at,omitempty"`
	FetchedAtUnix   int64  `json:"fetched_at,omitempty"`
	RemainingSecond int64  `json:"remaining_seconds,omitempty"`
	FetchCount      int64  `json:"fetch_count,omitempty"`
	Network         string `json:"network,omitempty"`
	Location        string `json:"location,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

// RotateTTLBounds clamps a provider-reported lifetime into a sane window.
func RotateTTLBounds(seconds int) (int, bool) {
	if seconds <= 0 {
		return 0, false
	}
	if seconds > DefaultRotateMaxTTLSeconds {
		return DefaultRotateMaxTTLSeconds, true
	}
	return seconds, true
}

package geminiweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	httpcloak "github.com/sardanioss/httpcloak/client"
)

type SessionParams struct {
	AccessToken string // SNlM0e (at)
	BuildLabel  string // cfb2h (bl)
	SessionID   string // FdrFJe (f.sid)
	Language    string // TuX5cc (hl)
	PushID      string // qKIAYe
}

// defaultStreamTimeout caps a single StreamGenerate call end to end. Gemini Web
// answers legitimately run for minutes (long thinking, tool calls), so it is far
// larger than the 60s client-wide timeout that the short RPC calls rely on.
const defaultStreamTimeout = 5 * time.Minute

type Client struct {
	mu             sync.RWMutex
	rotateMu       sync.Mutex
	lastRotated    time.Time
	cookieHeader   string
	cookiesMap     map[string]string
	proxyID        string
	proxy          string
	rotateURL      string
	appURL         string
	streamGenURL   string
	batchExecURL   string
	session        SessionParams
	modelSpecs     map[string]ModelSpec // alias -> spec, filled by DiscoverModels
	cloakClient    *httpcloak.Client
	streamTimeout  time.Duration
	onCookieUpdate func(newCookies string)
	quotaCache     *GeminiAccountQuotaSummary
	quotaCached    time.Time
	closed         bool
}

type ClientOptions struct {
	ProxyID            string
	Proxy              string
	Timeout            time.Duration
	InsecureSkipVerify bool
	ForceHTTP1         bool
	RotateURL          string
	AppURL             string
	StreamGenerateURL  string
	BatchExecuteURL    string
	// StreamTimeout overrides defaultStreamTimeout for StreamGenerate only.
	StreamTimeout time.Duration
}

func NewClient(rawCookies string, opts ...ClientOptions) (*Client, error) {
	var opt ClientOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	cookieMap, cookieHeader, err := ParseCookies(rawCookies)
	if err != nil {
		return nil, fmt.Errorf("invalid gemini cookies: %w", err)
	}

	cloakOpts := []httpcloak.Option{
		httpcloak.WithTimeout(60 * time.Second),
	}
	if opt.ForceHTTP1 {
		cloakOpts = append(cloakOpts, httpcloak.WithForceHTTP1())
	} else {
		cloakOpts = append(cloakOpts, httpcloak.WithForceHTTP2())
	}
	if opt.InsecureSkipVerify {
		cloakOpts = append(cloakOpts, httpcloak.WithInsecureSkipVerify())
	}
	if opt.RotateURL == "" && opt.AppURL == "" && opt.StreamGenerateURL == "" {
		cloakOpts = append(cloakOpts, httpcloak.WithTLSOnly())
	}
	if opt.Timeout > 0 {
		cloakOpts = append(cloakOpts, httpcloak.WithTimeout(opt.Timeout))
	}
	if opt.Proxy != "" {
		cloakOpts = append(cloakOpts, httpcloak.WithTCPProxy(opt.Proxy))
	}

	client := httpcloak.NewClient("chrome-150-windows", cloakOpts...)

	streamTimeout := opt.StreamTimeout
	if streamTimeout <= 0 {
		streamTimeout = defaultStreamTimeout
	}

	c := &Client{
		cookieHeader:  cookieHeader,
		cookiesMap:    cookieMap,
		proxyID:       opt.ProxyID,
		proxy:         opt.Proxy,
		rotateURL:     opt.RotateURL,
		appURL:        opt.AppURL,
		streamGenURL:  opt.StreamGenerateURL,
		batchExecURL:  opt.BatchExecuteURL,
		cloakClient:   client,
		streamTimeout: streamTimeout,
		modelSpecs:    make(map[string]ModelSpec),
	}

	return c, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.cloakClient != nil {
		c.cloakClient.Close()
	}
	return nil
}

func (c *Client) Session() SessionParams {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.session
}

func (c *Client) SetSession(session SessionParams) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = session
}

// discoveredSpecs returns a copy of the per-account models found by
// DiscoverModels, keyed by every alias Gemini Web reports for each model.
func (c *Client) discoveredSpecs() map[string]ModelSpec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]ModelSpec, len(c.modelSpecs))
	for k, v := range c.modelSpecs {
		out[k] = v
	}
	return out
}

func (c *Client) setDiscoveredSpecs(specs map[string]ModelSpec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.modelSpecs = specs
}

func (c *Client) CookieHeader() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cookieHeader
}

func (c *Client) CookiesMap() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	copyMap := make(map[string]string, len(c.cookiesMap))
	for k, v := range c.cookiesMap {
		copyMap[k] = v
	}
	return copyMap
}

func (c *Client) SetOnCookieUpdate(cb func(newCookies string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onCookieUpdate = cb
}

func (c *Client) UpdateCookie(name, value string) {
	c.mu.Lock()
	c.cookiesMap[name] = value
	var parts []string
	for k, v := range c.cookiesMap {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	c.cookieHeader = strings.Join(parts, "; ")
	cb := c.onCookieUpdate
	header := c.cookieHeader
	c.mu.Unlock()

	if cb != nil {
		cb(header)
	}
}

// ParseCookies supports:
// 1. Raw header: "__Secure-1PSID=...; __Secure-1PSIDTS=..."
// 2. JSON object: {"__Secure-1PSID": "..."} or {"cookies": {...}}
// 3. JSON array: [{"name": "__Secure-1PSID", "value": "..."}]
func ParseCookies(raw string) (map[string]string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", errors.New("empty cookies")
	}

	// 1. Try JSON object with "cookies" field
	var wrap struct {
		Cookies map[string]string `json:"cookies"`
	}
	if err := json.Unmarshal([]byte(raw), &wrap); err == nil && len(wrap.Cookies) > 0 {
		return formatCookieMap(wrap.Cookies)
	}

	// 2. Try plain JSON key-value map
	var flat map[string]string
	if err := json.Unmarshal([]byte(raw), &flat); err == nil && len(flat) > 0 {
		return formatCookieMap(flat)
	}

	// 3. Try JSON list of cookie objects
	var list []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err == nil && len(list) > 0 {
		m := make(map[string]string, len(list))
		for _, item := range list {
			if item.Name != "" {
				m[item.Name] = item.Value
			}
		}
		if len(m) > 0 {
			return formatCookieMap(m)
		}
	}

	// 4. Fallback: Parse semicolon-delimited cookie string
	pairs := strings.Split(raw, ";")
	m := make(map[string]string)
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx := strings.IndexByte(p, '=')
		if idx > 0 {
			k := strings.TrimSpace(p[:idx])
			v := strings.TrimSpace(p[idx+1:])
			m[k] = v
		}
	}
	if len(m) == 0 {
		return nil, "", errors.New("no valid cookie pairs found")
	}

	return formatCookieMap(m)
}

func formatCookieMap(m map[string]string) (map[string]string, string, error) {
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return m, strings.Join(parts, "; "), nil
}

func (c *Client) BuildDefaultHeaders() map[string][]string {
	return map[string][]string{
		"User-Agent":      {DefaultUserAgent},
		"Accept":          {"*/*"},
		"Accept-Language": {"en-US,en;q=0.9"},
		"Cookie":          {c.CookieHeader()},
		"Origin":          {BaseURL},
		"Referer":         {BaseURL + "/"},
		HeaderSameDomain:  {"1"},
	}
}

func (c *Client) Do(ctx context.Context, req *httpcloak.Request) (*httpcloak.Response, error) {
	c.mu.RLock()
	cloak := c.cloakClient
	c.mu.RUnlock()
	if cloak == nil {
		return nil, errors.New("gemini client closed")
	}
	return cloak.Do(ctx, req)
}

func (c *Client) DoStream(ctx context.Context, req *httpcloak.Request) (*httpcloak.StreamResponse, error) {
	c.mu.RLock()
	cloak := c.cloakClient
	c.mu.RUnlock()
	if cloak == nil {
		return nil, errors.New("gemini client closed")
	}
	return cloak.DoStream(ctx, req)
}

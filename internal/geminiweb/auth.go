package geminiweb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	httpcloak "github.com/sardanioss/httpcloak/client"

	"ds2api/internal/config"
)

type InitSessionOptions struct {
	SkipDiscovery bool
}

// InitSession fetches https://gemini.google.com/app and extracts 5 session parameters:
// SNlM0e (at), cfb2h (bl), FdrFJe (f.sid), TuX5cc (hl), and qKIAYe.
func (c *Client) InitSession(ctx context.Context, opts ...InitSessionOptions) (*SessionParams, error) {
	var opt InitSessionOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	headers := c.BuildDefaultHeaders()
	headers["Accept"] = []string{"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8"}

	req := &httpcloak.Request{
		Method:  http.MethodGet,
		URL:     c.getAppURL(),
		Headers: headers,
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("gemini init request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini init returned status %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read gemini init body: %w", err)
	}
	body := string(bodyBytes)

	// Check and update any rotated cookies from Set-Cookie headers
	c.checkSetCookies(resp.Headers)

	var session SessionParams

	if m := AccessTokenRe.FindStringSubmatch(body); len(m) > 1 {
		session.AccessToken = m[1]
	}
	if m := BuildLabelRe.FindStringSubmatch(body); len(m) > 1 {
		session.BuildLabel = m[1]
	}
	if m := SessionIDRe.FindStringSubmatch(body); len(m) > 1 {
		session.SessionID = m[1]
	}
	if m := LanguageRe.FindStringSubmatch(body); len(m) > 1 {
		session.Language = m[1]
	}
	if m := PushIDRe.FindStringSubmatch(body); len(m) > 1 {
		session.PushID = m[1]
	}

	if session.AccessToken == "" || session.BuildLabel == "" {
		return nil, errors.New("failed to extract required session parameters (SNlM0e / cfb2h); check cookies validity")
	}
	if session.Language == "" {
		session.Language = "en"
	}

	c.SetSession(session)

	// Model ids, tier capacity and capacity field are per-account and drift over
	// time, so they are discovered rather than hardcoded. A failure here is not
	// fatal: ResolveModelSpec falls back to the free-tier defaults.
	if !opt.SkipDiscovery {
		if _, err := c.DiscoverModels(ctx); err != nil {
			config.Logger.Warn("[geminiweb] model discovery failed, using fallback specs", "error", err)
		}
	}

	return &session, nil
}

// RotateCookies refreshes the __Secure-1PSIDTS cookie and returns its new value.
//
// The rotation endpoint lives on accounts.google.com - not gemini.google.com -
// and takes a fixed opaque JSON body rather than the usual access-token form.
// Posting the batchexecute-style request at a Gemini path never rotates anything.
func (c *Client) RotateCookies(ctx context.Context) (string, error) {
	headers := c.BuildDefaultHeaders()
	delete(headers, HeaderSameDomain)
	delete(headers, "Referer")
	headers["Content-Type"] = []string{"application/json"}
	headers["Origin"] = []string{RotateCookiesOrigin}

	req := &httpcloak.Request{
		Method:  http.MethodPost,
		URL:     c.getRotateURL(),
		Headers: headers,
		Body:    strings.NewReader(RotateCookiesBody),
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return "", fmt.Errorf("rotate cookies request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("%w: cookies are no longer valid", ErrUnauthenticated)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("rotate cookies status %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	return c.checkSetCookies(resp.Headers), nil
}

func (c *Client) checkSetCookies(headers map[string][]string) string {
	var newTS string
	toUpdate := make(map[string]string)
	for k, vals := range headers {
		if strings.EqualFold(k, "set-cookie") {
			for _, v := range vals {
				parts := strings.Split(v, ";")
				if len(parts) == 0 {
					continue
				}
				cookiePair := strings.TrimSpace(parts[0])
				idx := strings.IndexByte(cookiePair, '=')
				if idx > 0 {
					cName := strings.TrimSpace(cookiePair[:idx])
					cVal := strings.TrimSpace(cookiePair[idx+1:])
					if cName != "" && cVal != "" {
						toUpdate[cName] = cVal
						if cName == "__Secure-1PSIDTS" {
							newTS = cVal
						}
					}
				}
			}
		}
	}
	if len(toUpdate) > 0 {
		c.updateCookiesBatch(toUpdate)
	}
	return newTS
}

func (c *Client) updateCookiesBatch(updates map[string]string) {
	c.mu.Lock()
	changed := false
	for k, v := range updates {
		if cur, ok := c.cookiesMap[k]; !ok || cur != v {
			c.cookiesMap[k] = v
			changed = true
		}
	}
	if !changed {
		c.mu.Unlock()
		return
	}
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

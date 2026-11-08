package proxyrotate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"ds2api/internal/config"
)

// fetch asks the provider API for a fresh exit node. It always runs over the
// direct network path: routing the control-plane call through the very proxy we
// are trying to obtain would deadlock a broken pool.
func (m *Manager) fetch(ctx context.Context, normalized config.Proxy) (node, error) {
	rotate := normalized.Rotate
	if rotate == nil {
		return node{}, errors.New("proxy has no rotate configuration")
	}
	requestURL, err := RequestURL(*rotate)
	if err != nil {
		return node{}, err
	}

	timeout := m.fetchTimeout
	if timeout <= 0 {
		timeout = fetchTimeout
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return node{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DS2API-RotateProxy/1.0")

	response, err := m.httpClient.Do(req)
	if err != nil {
		return node{}, fmt.Errorf("proxy rotate request failed: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			config.Logger.Warn("[proxyrotate] close response body failed", "proxy_id", normalized.ID, "error", closeErr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return node{}, fmt.Errorf("proxy rotate read failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		preview := string(body)
		if len(preview) > 180 {
			preview = preview[:180] + "..."
		}
		return node{}, fmt.Errorf("proxy rotate endpoint returned HTTP %d: %s", response.StatusCode, preview)
	}

	resolved, err := ParseProviderResponse(body, *rotate, m.now())
	if err != nil {
		return node{}, err
	}
	resolved.proxy.ID = normalized.ID
	resolved.proxy.Name = normalized.Name
	return resolved, nil
}

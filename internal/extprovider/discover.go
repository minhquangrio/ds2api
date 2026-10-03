package extprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	discoverTimeout = 15 * time.Second
	maxBodyLimit    = int64(2 * 1024 * 1024) // 2 MiB
)

func RedactToken(err error, token string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	token = strings.TrimSpace(token)
	if token != "" && len(token) > 4 {
		msg = strings.ReplaceAll(msg, token, "[REDACTED]")
	}
	return errors.New(msg)
}

func DiscoverModels(ctx context.Context, baseURL, token string) ([]ProviderModel, error) {
	normBase := NormalizeBaseURL(baseURL)
	if normBase == "" {
		return nil, errors.New("invalid base url")
	}

	targetURL := normBase + "/models"
	baseParsed, err := url.Parse(normBase)
	if err != nil {
		return nil, RedactToken(err, token)
	}

	client := &http.Client{
		Timeout: discoverTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("stopped after 3 redirects")
			}
			if req.URL.Host != baseParsed.Host {
				return fmt.Errorf("redirect to different host (%s) rejected to prevent credential leak", req.URL.Host)
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, RedactToken(err, token)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "DS2API-ExternalProvider")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, RedactToken(err, token)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("extprovider: failed to close discover response body: %v", cerr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyLimit))
	if err != nil {
		return nil, RedactToken(err, token)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, RedactToken(fmt.Errorf("upstream returned status %d: %s", resp.StatusCode, string(body)), token)
	}

	models, err := parseModelsJSON(body)
	if err != nil {
		return nil, RedactToken(err, token)
	}

	return models, nil
}

func parseModelsJSON(data []byte) ([]ProviderModel, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid json response: %w", err)
	}

	var list []any
	switch v := raw.(type) {
	case []any:
		list = v
	case map[string]any:
		if d, ok := v["data"].([]any); ok {
			list = d
		} else if m, ok := v["models"].([]any); ok {
			list = m
		} else if i, ok := v["items"].([]any); ok {
			list = i
		} else if r, ok := v["result"].([]any); ok {
			list = r
		} else {
			return nil, errors.New("no recognized models array ('data', 'models', 'items', 'result') found")
		}
	default:
		return nil, errors.New("unexpected json format")
	}

	var models []ProviderModel
	seen := make(map[string]bool)

	for _, item := range list {
		mMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		id, _ := mMap["id"].(string)
		if id == "" {
			if name, ok := mMap["name"].(string); ok {
				id = name
			}
		}
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true

		displayName, _ := mMap["display_name"].(string)
		if displayName == "" {
			displayName, _ = mMap["name"].(string)
		}

		ctxWindow := 0
		if cw, ok := mMap["context_window"].(float64); ok {
			ctxWindow = int(cw)
		} else if cw, ok := mMap["max_context_tokens"].(float64); ok {
			ctxWindow = int(cw)
		} else if cw, ok := mMap["context_length"].(float64); ok {
			ctxWindow = int(cw)
		}

		models = append(models, ProviderModel{
			ID:                 id,
			Name:               displayName,
			ContextWindow:      ctxWindow,
			Capabilities:       []string{},
			CapabilitiesSource: "catalog",
			InspectStatus:      "unknown",
		})
	}

	return models, nil
}

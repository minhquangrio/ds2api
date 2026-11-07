package muse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/assistantturn"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

type Client struct {
	WorkerURL      string
	Secret         string
	ProxyURL       string
	Account        config.Account
	HTTPClient     *http.Client
	OnCookieUpdate func(string)
}

func NewClient(workerURL, secret, proxyURL string, acc config.Account) *Client {
	url := strings.TrimRight(strings.TrimSpace(workerURL), "/")
	if url == "" {
		url = "http://127.0.0.1:8765"
	}
	return &Client{
		WorkerURL:  url,
		Secret:     strings.TrimSpace(secret),
		ProxyURL:   strings.TrimSpace(proxyURL),
		Account:    acc,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) buildHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.Secret != "" {
		req.Header.Set("X-Worker-Secret", c.Secret)
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
}

func (c *Client) buildPayload(stdReq promptcompat.StandardRequest, stream bool) map[string]any {
	model := workerModel(stdReq)
	prompt := workerPrompt(stdReq)

	return map[string]any{
		"request_id": fmt.Sprintf("muse_%d", time.Now().UnixNano()),
		"account": map[string]any{
			"account_id": c.Account.Identifier(),
			"cookies":    c.Account.Cookies,
			"proxy":      c.ProxyURL,
		},
		"model":    model,
		"prompt":   prompt,
		"stream":   stream,
		"messages": stdReq.PromptMessages,
		"options": map[string]any{
			"thinking": stdReq.Thinking,
		},
	}
}

// Health checks worker availability.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.WorkerURL+"/health", nil)
	if err != nil {
		return err
	}
	c.buildHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMuseWorkerUnavailable, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("muse: failed to close health response body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("muse worker health check returned status %d", resp.StatusCode)
	}
	return nil
}

// ExecuteChat sends a non-streaming chat completion request to the worker.
func (c *Client) ExecuteChat(ctx context.Context, stdReq promptcompat.StandardRequest) (assistantturn.Turn, error) {
	if err := validateChatModel(workerModel(stdReq)); err != nil {
		return assistantturn.Turn{}, err
	}
	payload := c.buildPayload(stdReq, false)
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return assistantturn.Turn{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WorkerURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return assistantturn.Turn{}, err
	}
	c.buildHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return assistantturn.Turn{}, fmt.Errorf("%w: %v", ErrMuseWorkerUnavailable, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("muse: failed to close chat response body: %v", cerr)
		}
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return assistantturn.Turn{}, err
	}

	if resp.StatusCode != http.StatusOK {
		var errData struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		_ = json.Unmarshal(respBody, &errData)
		errMsg := errData.Error
		if errMsg == "" {
			errMsg = string(respBody)
		}
		if resp.StatusCode == http.StatusTooManyRequests || strings.Contains(strings.ToLower(errMsg), "rate limit") {
			return assistantturn.Turn{}, fmt.Errorf("%w: %s", ErrMuseThrottled, errMsg)
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return assistantturn.Turn{}, fmt.Errorf("%w: %s", ErrMuseUnauthorized, errMsg)
		}
		return assistantturn.Turn{}, fmt.Errorf("muse worker error (%d): %s", resp.StatusCode, errMsg)
	}

	var res workerCompletion
	if err := json.Unmarshal(respBody, &res); err != nil {
		return assistantturn.Turn{}, fmt.Errorf("failed to parse muse worker response: %w", err)
	}

	if res.Cookies != nil && c.OnCookieUpdate != nil {
		c.OnCookieUpdate(*res.Cookies)
	}
	turn := buildWorkerTurn(stdReq, res)
	return turn, turnError(turn)
}

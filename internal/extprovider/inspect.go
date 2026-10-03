package extprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	inspectTimeout = 20 * time.Second

	// maxInspectDetailBytes bounds what is persisted (and shown in the console)
	// from an upstream error body.
	maxInspectDetailBytes = 2 * 1024
)

func classifyError(statusCode int, body string) string {
	lower := strings.ToLower(body)
	if statusCode == 401 || statusCode == 403 {
		return "auth_error"
	}
	if statusCode == 429 || statusCode == 408 || statusCode == 409 || statusCode == 425 || statusCode >= 500 ||
		strings.Contains(lower, "rate limit") || strings.Contains(lower, "quota exceeded") || strings.Contains(lower, "too many requests") {
		return "busy"
	}
	if strings.Contains(lower, "not supported") || strings.Contains(lower, "unsupported") || strings.Contains(lower, "invalid parameter") {
		return "incompatible"
	}
	if statusCode == 404 {
		return "unavailable"
	}
	return "error"
}

func truncateDetail(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= maxInspectDetailBytes {
		return raw
	}
	return raw[:maxInspectDetailBytes] + "… (truncated)"
}

// probeResult carries the parsed assistant message so callers can verify that
// the model actually produced the capability, not merely that the endpoint
// accepted the request. OpenAI-compatible servers commonly return 200 while
// silently ignoring tools, reasoning_effort or image parts.
type probeResult struct {
	Status     string
	HTTPStatus int
	Detail     string
	Message    map[string]any
}

func (r probeResult) OK() bool { return r.Status == "ready" }

func probeChatCapability(ctx context.Context, client *http.Client, baseURL, token string, body map[string]any) probeResult {
	normBase := NormalizeBaseURL(baseURL)
	targetURL := normBase + "/chat/completions"

	data, err := json.Marshal(body)
	if err != nil {
		return probeResult{Status: "error", Detail: err.Error()}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(data))
	if err != nil {
		return probeResult{Status: "error", Detail: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DS2API-ExternalProvider")
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	resp, err := client.Do(req)
	if err != nil {
		return probeResult{Status: "unavailable", Detail: RedactToken(err, token).Error()}
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("extprovider: failed to close probe body: %v", cerr)
		}
	}()

	respBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if readErr != nil {
		return probeResult{Status: "error", HTTPStatus: resp.StatusCode, Detail: readErr.Error()}
	}
	respStr := string(respBytes)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		st := classifyError(resp.StatusCode, respStr)
		detail := truncateDetail(RedactToken(fmt.Errorf("%s", respStr), token).Error())
		return probeResult{Status: st, HTTPStatus: resp.StatusCode, Detail: detail}
	}

	return probeResult{
		Status:     "ready",
		HTTPStatus: resp.StatusCode,
		Message:    firstAssistantMessage(respBytes),
	}
}

// firstAssistantMessage extracts choices[0].message from a chat completion,
// tolerating responses wrapped in an OpenAI-style envelope.
func firstAssistantMessage(body []byte) map[string]any {
	var parsed struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Choices) == 0 {
		return nil
	}
	return parsed.Choices[0].Message
}

func messageHasToolCalls(msg map[string]any) bool {
	if msg == nil {
		return false
	}
	if calls, ok := msg["tool_calls"].([]any); ok && len(calls) > 0 {
		return true
	}
	_, ok := msg["function_call"].(map[string]any)
	return ok
}

func messageHasReasoning(msg map[string]any) bool {
	if msg == nil {
		return false
	}
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if s, ok := msg[key].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

func messageHasText(msg map[string]any) bool {
	if msg == nil {
		return false
	}
	s, ok := msg["content"].(string)
	return ok && strings.TrimSpace(s) != ""
}

func InspectModel(ctx context.Context, baseURL, token string, existingModel ProviderModel) ProviderModel {
	client := &http.Client{Timeout: inspectTimeout}
	modelID := existingModel.ID

	baseBody := map[string]any{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 5,
	}

	base := probeChatCapability(ctx, client, baseURL, token, baseBody)
	if !base.OK() {
		// A transient failure must not erase capabilities confirmed earlier.
		if (base.Status == "busy" || base.Status == "unavailable") && len(existingModel.Capabilities) > 0 {
			existingModel.CapabilitiesSource = "last_successful"
		}
		existingModel.InspectStatus = base.Status
		existingModel.InspectError = base.Detail
		existingModel.LastInspectAt = time.Now().Unix()
		return existingModel
	}

	var caps []string

	// Tool calling is only confirmed when the model actually returns a call.
	toolsBody := map[string]any{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "Call the provider_probe tool with query=\"ok\". Do not answer with text."},
		},
		"max_tokens": 64,
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "provider_probe",
					"description": "test probe",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]string{"type": "string"},
						},
						"required": []string{"query"},
					},
				},
			},
		},
		"tool_choice": "auto",
	}
	if tools := probeChatCapability(ctx, client, baseURL, token, toolsBody); tools.OK() && messageHasToolCalls(tools.Message) {
		caps = append(caps, "tools")
	}

	// Reasoning is only confirmed when reasoning content comes back.
	reasoningBody := map[string]any{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "1+1="},
		},
		"max_tokens":       64,
		"reasoning_effort": "low",
	}
	if reasoning := probeChatCapability(ctx, client, baseURL, token, reasoningBody); reasoning.OK() && messageHasReasoning(reasoning.Message) {
		caps = append(caps, "reasoning")
	}

	// Vision is only confirmed when the model still answers with text while an
	// image part is present (a 200 alone says nothing about image support).
	visionBody := map[string]any{
		"model": modelID,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "Reply with exactly OK."},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
						},
					},
				},
			},
		},
		"max_tokens": 16,
	}
	if vision := probeChatCapability(ctx, client, baseURL, token, visionBody); vision.OK() && messageHasText(vision.Message) {
		caps = append(caps, "vision")
	}

	existingModel.Capabilities = caps
	existingModel.CapabilitiesSource = "inspected"
	existingModel.InspectStatus = "ready"
	existingModel.InspectError = ""
	existingModel.LastInspectAt = time.Now().Unix()
	return existingModel
}

func InspectProvider(ctx context.Context, p *Provider) error {
	if p == nil {
		return nil
	}

	models := p.Models
	if len(models) == 0 {
		return nil
	}

	limit := len(models)
	if limit > 20 {
		limit = 20
	}

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			inspected := InspectModel(ctx, p.BaseURL, p.Token, models[idx])

			mu.Lock()
			models[idx] = inspected
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	p.Models = models

	logItem := InspectionLog{
		ID:        fmt.Sprintf("log_%d", time.Now().UnixNano()),
		ModelID:   fmt.Sprintf("%d models", limit),
		Status:    "completed",
		Details:   fmt.Sprintf("Inspected %d models", limit),
		Timestamp: time.Now().Unix(),
	}
	p.InspectionHistory = append([]InspectionLog{logItem}, p.InspectionHistory...)
	if len(p.InspectionHistory) > 50 {
		p.InspectionHistory = p.InspectionHistory[:50]
	}

	return nil
}

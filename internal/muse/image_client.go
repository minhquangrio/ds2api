package muse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/mediacompat"
)

// workerMediaResponse is the worker's single-shot media envelope. b64_json is
// authoritative; url/media_id only appear when the operator configured a
// public media base on the worker.
type workerMediaResponse struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	Model     string `json:"model"`
	Created   int64  `json:"created"`
	Data      []struct {
		B64JSON     *string `json:"b64_json"`
		URL         *string `json:"url"`
		MediaID     *string `json:"media_id"`
		ContentType string  `json:"content_type"`
		Width       int     `json:"width"`
		Height      int     `json:"height"`
	} `json:"data"`
	Usage     map[string]any `json:"usage"`
	Cookies   *string        `json:"cookies"`
	ThreadURL string         `json:"thread_url"`
	Error     string         `json:"error"`
	Code      string         `json:"code"`
}

func (c *Client) buildImagePayload(req mediacompat.ImageRequest) map[string]any {
	references := make([]map[string]any, 0, len(req.References))
	for _, ref := range req.References {
		references = append(references, map[string]any{
			"filename":     ref.Filename,
			"content_type": ref.ContentType,
			"data_base64":  base64.StdEncoding.EncodeToString(ref.Data),
		})
	}
	mode := "generation"
	if req.IsEdit() {
		mode = "edit"
	}
	payload := map[string]any{
		"request_id":      fmt.Sprintf("muse_img_%d", time.Now().UnixNano()),
		"account":         c.accountPayload(),
		"model":           workerMediaModel(req.ResolvedModel, req.RequestedModel),
		"prompt":          req.Prompt,
		"mode":            mode,
		"n":               req.N,
		"response_format": req.ResponseFormat,
		"caller_id":       req.CallerID,
		"references":      references,
		"options":         map[string]any{},
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}
	return payload
}

func (c *Client) accountPayload() map[string]any {
	return map[string]any{
		"account_id": c.Account.Identifier(),
		"cookies":    c.Account.Cookies,
		"proxy":      c.ProxyURL,
	}
}

// ExecuteImage performs one image generation or edit through the worker.
func (c *Client) ExecuteImage(ctx context.Context, req mediacompat.ImageRequest) (mediacompat.MediaResult, error) {
	if err := validateImageModel(workerMediaModel(req.ResolvedModel, req.RequestedModel)); err != nil {
		return mediacompat.MediaResult{}, err
	}
	path := "/v1/images/generations"
	if req.IsEdit() {
		path = "/v1/images/edits"
	}
	body, err := json.Marshal(c.buildImagePayload(req))
	if err != nil {
		return mediacompat.MediaResult{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WorkerURL+path, bytes.NewReader(body))
	if err != nil {
		return mediacompat.MediaResult{}, err
	}
	c.buildHeaders(httpReq)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return mediacompat.MediaResult{}, fmt.Errorf("%w: %v", ErrMuseWorkerUnavailable, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("muse: failed to close image response body: %v", closeErr)
		}
	}()

	// A generated image in base64 is far larger than a chat turn, so the read
	// limit matches the worker media caps instead of the chat 10 MiB default.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, mediaResponseLimit))
	if err != nil {
		return mediacompat.MediaResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return mediacompat.MediaResult{}, workerError(resp.StatusCode, raw)
	}

	var parsed workerMediaResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return mediacompat.MediaResult{}, fmt.Errorf("failed to parse muse worker media response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return mediacompat.MediaResult{}, fmt.Errorf("%w: worker returned no media", ErrMuseWorkerUnavailable)
	}
	if parsed.Cookies != nil && c.OnCookieUpdate != nil {
		c.OnCookieUpdate(*parsed.Cookies)
	}
	result := mediacompat.MediaResult{
		Model:     parsed.Model,
		Created:   parsed.Created,
		Usage:     parsed.Usage,
		ThreadURL: parsed.ThreadURL,
		Items:     make([]mediacompat.MediaItem, 0, len(parsed.Data)),
	}
	if result.Created == 0 {
		result.Created = time.Now().Unix()
	}
	for _, item := range parsed.Data {
		mediaItem := mediacompat.MediaItem{ContentType: item.ContentType, Width: item.Width, Height: item.Height}
		if item.B64JSON != nil {
			mediaItem.B64JSON = *item.B64JSON
		}
		if item.URL != nil {
			mediaItem.URL = *item.URL
		}
		if item.MediaID != nil {
			mediaItem.MediaID = *item.MediaID
		}
		result.Items = append(result.Items, mediaItem)
	}
	return result, nil
}

func workerError(status int, raw []byte) error {
	var errData struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(raw, &errData)
	message := errData.Error
	if message == "" {
		message = truncateBytes(raw, 400)
	}
	if errData.Code != "" {
		message = fmt.Sprintf("%s: %s", errData.Code, message)
	}
	switch {
	case status == http.StatusTooManyRequests || strings.Contains(strings.ToLower(message), "rate limit"):
		return fmt.Errorf("%w: %s", ErrMuseThrottled, message)
	case status == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrMuseUnauthorized, message)
	default:
		return fmt.Errorf("muse worker error (%d): %s", status, message)
	}
}

func truncateBytes(raw []byte, limit int) string {
	if len(raw) <= limit {
		return string(raw)
	}
	return string(raw[:limit])
}

var mediaResponseLimit int64 = 64 << 20

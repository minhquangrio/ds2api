package muse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ds2api/internal/mediacompat"
)

// ErrMuseJobNotFound is returned when the worker has no record for a task id,
// including the case where the task belongs to a different caller.
var ErrMuseJobNotFound = errors.New("muse media job not found")

type workerTaskEnvelope struct {
	Status string            `json:"status"`
	Task   workerVideoTask   `json:"task"`
	Error  string            `json:"error"`
	Code   string            `json:"code"`
	Data   []workerVideoTask `json:"data"`
}

type workerVideoTask struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	Stage       string  `json:"stage"`
	Model       string  `json:"model"`
	Progress    int     `json:"progress"`
	CreatedAt   float64 `json:"created_at"`
	UpdatedAt   float64 `json:"updated_at"`
	Error       *string `json:"error"`
	ErrorCode   *string `json:"error_code"`
	MediaID     *string `json:"media_id"`
	ContentType string  `json:"content_type"`
	SizeBytes   int64   `json:"size_bytes"`
	Duration    *int    `json:"duration"`
}

func (t workerVideoTask) toShared() mediacompat.VideoTask {
	task := mediacompat.VideoTask{
		ID: t.ID, Status: t.Status, Stage: t.Stage, Model: t.Model, Progress: t.Progress,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, ContentType: t.ContentType, SizeBytes: t.SizeBytes,
	}
	if t.Error != nil {
		task.Error = *t.Error
	}
	if t.ErrorCode != nil {
		task.ErrorCode = *t.ErrorCode
	}
	if t.MediaID != nil {
		task.MediaID = *t.MediaID
	}
	if t.Duration != nil {
		task.DurationSeconds = *t.Duration
	}
	if task.Model == "" {
		task.Model = ModelMuseVideo
	}
	if task.CreatedAt == 0 {
		task.CreatedAt = float64(time.Now().Unix())
	}
	return task
}

func (c *Client) buildVideoPayload(req mediacompat.VideoRequest) map[string]any {
	references := make([]map[string]any, 0, len(req.References))
	for _, ref := range req.References {
		references = append(references, map[string]any{
			"filename":     ref.Filename,
			"content_type": ref.ContentType,
			"data_base64":  base64.StdEncoding.EncodeToString(ref.Data),
		})
	}
	payload := map[string]any{
		"request_id": fmt.Sprintf("muse_vid_%d", time.Now().UnixNano()),
		"account":    c.accountPayload(),
		"model":      workerMediaModel(req.ResolvedModel, req.RequestedModel),
		"prompt":     req.Prompt,
		"caller_id":  req.CallerID,
		"references": references,
		"options":    map[string]any{},
	}
	if req.DurationSeconds > 0 {
		payload["duration"] = req.DurationSeconds
	}
	if req.Size != "" {
		payload["size"] = req.Size
	}
	return payload
}

// CreateVideo submits an asynchronous video job and returns its initial record.
func (c *Client) CreateVideo(ctx context.Context, req mediacompat.VideoRequest) (mediacompat.VideoTask, error) {
	if err := validateVideoModel(workerMediaModel(req.ResolvedModel, req.RequestedModel)); err != nil {
		return mediacompat.VideoTask{}, err
	}
	body, err := json.Marshal(c.buildVideoPayload(req))
	if err != nil {
		return mediacompat.VideoTask{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WorkerURL+"/v1/videos", strings.NewReader(string(body)))
	if err != nil {
		return mediacompat.VideoTask{}, err
	}
	c.buildHeaders(httpReq)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return mediacompat.VideoTask{}, fmt.Errorf("%w: %v", ErrMuseWorkerUnavailable, err)
	}
	defer closeWorkerBody(resp, "video create")

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return mediacompat.VideoTask{}, err
	}
	// The worker answers create with 200; accept any 2xx so a future 201 from
	// the worker does not look like a failure to the gateway.
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return mediacompat.VideoTask{}, workerError(resp.StatusCode, raw)
	}
	var envelope workerTaskEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return mediacompat.VideoTask{}, fmt.Errorf("failed to parse muse video task response: %w", err)
	}
	if strings.TrimSpace(envelope.Task.ID) == "" {
		return mediacompat.VideoTask{}, fmt.Errorf("%w: worker returned no video task", ErrMuseWorkerUnavailable)
	}
	return envelope.Task.toShared(), nil
}

// PollVideo reads one task record. A foreign caller id yields ErrMuseJobNotFound.
func (c *Client) PollVideo(ctx context.Context, taskID, callerID string) (mediacompat.VideoTask, error) {
	resp, err := c.doWorkerGet(ctx, "/v1/videos/"+url.PathEscape(taskID), callerID)
	if err != nil {
		return mediacompat.VideoTask{}, err
	}
	defer closeWorkerBody(resp, "video poll")
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return mediacompat.VideoTask{}, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return mediacompat.VideoTask{}, ErrMuseJobNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return mediacompat.VideoTask{}, workerError(resp.StatusCode, raw)
	}
	var envelope workerTaskEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return mediacompat.VideoTask{}, fmt.Errorf("failed to parse muse video task response: %w", err)
	}
	if strings.TrimSpace(envelope.Task.ID) == "" {
		return mediacompat.VideoTask{}, ErrMuseJobNotFound
	}
	return envelope.Task.toShared(), nil
}

// FetchVideoContent downloads the finished media bytes for a task.
func (c *Client) FetchVideoContent(ctx context.Context, taskID, callerID string) ([]byte, string, error) {
	resp, err := c.doWorkerGet(ctx, "/v1/videos/"+url.PathEscape(taskID)+"/content", callerID)
	if err != nil {
		return nil, "", err
	}
	defer closeWorkerBody(resp, "video content")
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, "", ErrMuseJobNotFound
	case http.StatusGone:
		return nil, "", fmt.Errorf("%w: media expired", ErrMuseWorkerUnavailable)
	case http.StatusConflict:
		return nil, "", fmt.Errorf("%w: video job is not completed", ErrMuseWorkerUnavailable)
	default:
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return nil, "", readErr
		}
		return nil, "", workerError(resp.StatusCode, raw)
	}
	contentType := resp.Header.Get("Content-Type")
	data, err := io.ReadAll(io.LimitReader(resp.Body, mediaResponseLimit))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("%w: worker returned empty media", ErrMuseWorkerUnavailable)
	}
	return data, contentType, nil
}

// FetchMediaBytes reads one stored image blob by id (url response format).
func (c *Client) FetchMediaBytes(ctx context.Context, mediaID string) ([]byte, string, error) {
	resp, err := c.doWorkerGet(ctx, "/v1/media/"+url.PathEscape(mediaID), "")
	if err != nil {
		return nil, "", err
	}
	defer closeWorkerBody(resp, "media bytes")
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil, "", ErrMuseJobNotFound
	}
	if resp.StatusCode != http.StatusOK {
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return nil, "", readErr
		}
		return nil, "", workerError(resp.StatusCode, raw)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, mediaResponseLimit))
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// ListVideos returns the calling key's video jobs, newest first.
func (c *Client) ListVideos(ctx context.Context, callerID string) ([]mediacompat.VideoTask, error) {
	resp, err := c.doWorkerGet(ctx, "/v1/videos", callerID)
	if err != nil {
		return nil, err
	}
	defer closeWorkerBody(resp, "video list")
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, workerError(resp.StatusCode, raw)
	}
	var envelope workerTaskEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse muse video list response: %w", err)
	}
	tasks := make([]mediacompat.VideoTask, 0, len(envelope.Data))
	for _, item := range envelope.Data {
		tasks = append(tasks, item.toShared())
	}
	return tasks, nil
}

func (c *Client) doWorkerGet(ctx context.Context, path, callerID string) (*http.Response, error) {
	target := c.WorkerURL + path
	if callerID != "" {
		if strings.Contains(target, "?") {
			target += "&caller_id=" + url.QueryEscape(callerID)
		} else {
			target += "?caller_id=" + url.QueryEscape(callerID)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	c.buildHeaders(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMuseWorkerUnavailable, err)
	}
	return resp, nil
}

func closeWorkerBody(resp *http.Response, op string) {
	if err := resp.Body.Close(); err != nil {
		log.Printf("muse: failed to close %s response body: %v", op, err)
	}
}

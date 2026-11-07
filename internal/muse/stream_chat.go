package muse

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"ds2api/internal/assistantturn"
	"ds2api/internal/promptcompat"
)

// StreamChat shares finalization and empty-output policy with ExecuteTurn.
// Tool requests are buffered until detection completes; plain text stays live.
func (c *Client) StreamChat(ctx context.Context, req promptcompat.StandardRequest, onDelta func(string, string) error) (assistantturn.Turn, error) {
	if err := validateChatModel(workerModel(req)); err != nil {
		return assistantturn.Turn{}, err
	}
	emitted := false
	callback := func(text, thinking string) error {
		if (text == "" && thinking == "") || onDelta == nil {
			return nil
		}
		emitted = true
		return onDelta(text, thinking)
	}
	for attempt := 0; ; attempt++ {
		turn, err := c.streamChatOnce(ctx, req, callback)
		if emitted || !retryEmpty(turn, err, attempt) {
			return turn, err
		}
		req = emptyRetryRequest(req)
	}
}

func (c *Client) streamChatOnce(ctx context.Context, stdReq promptcompat.StandardRequest, onDelta func(string, string) error) (assistantturn.Turn, error) {
	body, err := json.Marshal(c.buildPayload(stdReq, true))
	if err != nil {
		return assistantturn.Turn{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WorkerURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return assistantturn.Turn{}, err
	}
	c.buildHeaders(req)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return assistantturn.Turn{}, fmt.Errorf("%w: %v", ErrMuseWorkerUnavailable, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("muse: failed to close stream response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if readErr != nil {
			return assistantturn.Turn{}, readErr
		}
		sentinel := ErrMuseWorkerUnavailable
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			sentinel = ErrMuseThrottled
		case http.StatusUnauthorized:
			sentinel = ErrMuseUnauthorized
		}
		return assistantturn.Turn{}, fmt.Errorf("%w (%d): %s", sentinel, resp.StatusCode, body)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	var text, reasoning strings.Builder
	var event string
	var completed workerCompletion
	var streamErr error
	terminal := false
	buffered := len(stdReq.ToolNames) > 0
	emit := func(txt, think string) error {
		if buffered || onDelta == nil {
			return nil
		}
		if !stdReq.Thinking {
			think = ""
		}
		return onDelta(txt, think)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		switch event {
		case "delta":
			var delta struct {
				Text      string `json:"text_delta"`
				Reasoning string `json:"reasoning_delta"`
			}
			if streamErr = json.Unmarshal(data, &delta); streamErr == nil {
				text.WriteString(delta.Text)
				reasoning.WriteString(delta.Reasoning)
				streamErr = emit(delta.Text, delta.Reasoning)
			}
		case "completed":
			streamErr = json.Unmarshal(data, &completed)
			terminal = streamErr == nil
		case "error":
			var failure struct {
				Error  string `json:"error"`
				Status int    `json:"status"`
			}
			if streamErr = json.Unmarshal(data, &failure); streamErr == nil {
				sentinel := ErrMuseWorkerUnavailable
				switch failure.Status {
				case http.StatusTooManyRequests:
					sentinel = ErrMuseThrottled
				case http.StatusUnauthorized:
					sentinel = ErrMuseUnauthorized
				}
				streamErr = fmt.Errorf("%w: %s", sentinel, failure.Error)
			}
		}
		if terminal || streamErr != nil {
			break
		}
	}
	if streamErr == nil && !terminal {
		streamErr = scanner.Err()
		if streamErr == nil {
			streamErr = io.ErrUnexpectedEOF
		}
	}
	// Reconcile completed-only workers and final snapshots with deltas.
	for _, pair := range []struct {
		final    string
		buf      *strings.Builder
		thinking bool
	}{
		{completed.Text, &text, false}, {completed.Reasoning, &reasoning, true},
	} {
		if streamErr != nil || pair.final == "" {
			continue
		}
		if !strings.HasPrefix(pair.final, pair.buf.String()) {
			streamErr = fmt.Errorf("muse completed snapshot disagrees with deltas")
			break
		}
		tail := pair.final[pair.buf.Len():]
		pair.buf.WriteString(tail)
		if pair.thinking {
			streamErr = emit("", tail)
		} else {
			streamErr = emit(tail, "")
		}
	}
	completed.Text, completed.Reasoning = text.String(), reasoning.String()
	turn := buildWorkerTurn(stdReq, completed)
	if streamErr != nil {
		turn.StopReason = assistantturn.StopReasonError
		return turn, streamErr
	}
	if completed.Cookies != nil && c.OnCookieUpdate != nil {
		c.OnCookieUpdate(*completed.Cookies)
	}
	if err := turnError(turn); err != nil {
		return turn, err
	}
	if buffered && len(turn.ToolCalls) == 0 && onDelta != nil {
		if err := onDelta(turn.Text, turn.Thinking); err != nil {
			return turn, err
		}
	}
	return turn, nil
}

package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/promptcompat"
)

func prepareCodexStream(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (*http.Response, error) {
	body, err := BuildResponsesBodyWithDefault(stdReq, client.DefaultModel())
	if err != nil {
		return nil, err
	}

	payloadBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CodexResponsesURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.DoStream(req)
	if err != nil {
		return nil, fmt.Errorf("codex request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				log.Printf("codex: failed to close error response body: %v", cerr)
			}
		}()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, fmt.Errorf("codex upstream error (%d): %s", resp.StatusCode, string(b))
	}

	return resp, nil
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func emitSSEJSON(w http.ResponseWriter, data any) {
	b, err := json.Marshal(data)
	if err == nil {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
	}
}

// streamOutcome carries what the stream learned from the upstream terminal
// event so the caller can report accurate usage and finish reasons.
type streamOutcome struct {
	Terminal StreamTerminal
	SawTerm  bool
}

func readStream(ctx context.Context, resp *http.Response, onEvent func(Event) error) (streamOutcome, error) {
	var out streamOutcome
	parser := NewStreamParser()
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			for _, ev := range parser.Feed(buf[:n]) {
				if term, ok := ev.ParseTerminal(); ok && !term.Failed {
					out.Terminal = term
					out.SawTerm = true
				} else if term, ok := ev.ParseTerminal(); ok && term.Failed {
					out.Terminal = term
					out.SawTerm = true
					if err := onEvent(ev); err != nil {
						return out, err
					}
					return out, fmt.Errorf("codex upstream ended with response.failed: %s", term.ErrorMessage)
				}
				if err := onEvent(ev); err != nil {
					return out, err
				}
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return out, nil
			}
			if errors.Is(rerr, context.Canceled) || errors.Is(rerr, context.DeadlineExceeded) {
				return out, rerr
			}
			// A truncated stream must not look like a clean finish: surface it
			// so the caller records an error instead of a successful turn.
			return out, fmt.Errorf("codex stream interrupted: %w", rerr)
		}
	}
}

func chatFinishReason(out streamOutcome, hasToolCalls bool) string {
	if out.SawTerm && out.Terminal.Type == "response.incomplete" {
		return "length"
	}
	if hasToolCalls {
		return "tool_calls"
	}
	return "stop"
}

func StreamOpenAIChat(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	resp, err := prepareCodexStream(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close stream body: %v", cerr)
		}
	}()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	id := fmt.Sprintf("chatcmpl-codex-%d", time.Now().UnixNano())
	created := time.Now().Unix()

	emitChunk := func(delta map[string]any, finish any) {
		emitSSEJSON(w, map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": stdReq.ResponseModel,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		if canFlush {
			flusher.Flush()
		}
	}

	emitChunk(map[string]any{"role": "assistant"}, nil)

	tools := NewToolCallTracker()
	var thinkBuf, textBuf strings.Builder

	out, streamErr := readStream(ctx, resp, func(ev Event) error {
		if rsn, ok := ev.ParseReasoningDelta(); ok {
			thinkBuf.WriteString(rsn)
			emitChunk(map[string]any{"reasoning_content": rsn}, nil)
		}
		if txt, ok := ev.ParseTextDelta(); ok {
			textBuf.WriteString(txt)
			emitChunk(map[string]any{"content": txt}, nil)
		}

		if ev.Type == "response.output_item.added" || ev.Type == "response.output_item.done" {
			var itemWrap struct {
				Item ResponsesOutputItem `json:"item"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &itemWrap); err == nil && itemWrap.Item.Type == "function_call" {
				callID := itemWrap.Item.CallID
				if callID == "" {
					callID = itemWrap.Item.ID
				}
				idx := tools.IndexFor(callID)
				tools.SetName(callID, itemWrap.Item.Name)
				if ev.Type == "response.output_item.added" {
					emitChunk(map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": idx,
								"id":    callID,
								"type":  "function",
								"function": map[string]any{
									"name":      itemWrap.Item.Name,
									"arguments": "",
								},
							},
						},
					}, nil)
				}
			}
		}

		if callID, delta, ok := ev.ParseFunctionCallArgumentsDelta(); ok {
			idx := tools.IndexFor(callID)
			tools.AppendArgs(callID, delta)
			emitChunk(map[string]any{
				"tool_calls": []any{
					map[string]any{
						"index": idx,
						"id":    callID,
						"function": map[string]any{
							"arguments": delta,
						},
					},
				},
			}, nil)
		}
		return nil
	})

	if streamErr != nil {
		return thinkBuf.String(), textBuf.String(), streamErr
	}

	emitChunk(map[string]any{}, chatFinishReason(out, tools.Len() > 0))
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}

	return thinkBuf.String(), textBuf.String(), nil
}

func StreamResponsesAPI(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) error {
	resp, err := prepareCodexStream(ctx, client, stdReq)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close stream body: %v", cerr)
		}
	}()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)

	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return nil
			}
			return rerr
		}
	}
}

func StreamClaudeMessages(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	resp, err := prepareCodexStream(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close stream body: %v", cerr)
		}
	}()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	msgID := fmt.Sprintf("msg_codex_%d", time.Now().UnixNano())

	// Upstream usage is only known once the terminal event arrives, so the
	// opening frame reports zeros rather than a guessed token count.
	sendClaudeEvent(w, flusher, canFlush, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": msgID, "type": "message", "role": "assistant", "model": stdReq.ResponseModel,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})

	blockIndex := 0
	thinkingBlockOpen := false
	textBlockOpen := false
	openToolBlocks := make(map[string]int)

	closeThinking := func() {
		if thinkingBlockOpen {
			sendClaudeEvent(w, flusher, canFlush, "content_block_stop", map[string]any{
				"type": "content_block_stop", "index": blockIndex,
			})
			thinkingBlockOpen = false
			blockIndex++
		}
	}
	closeText := func() {
		if textBlockOpen {
			sendClaudeEvent(w, flusher, canFlush, "content_block_stop", map[string]any{
				"type": "content_block_stop", "index": blockIndex,
			})
			textBlockOpen = false
			blockIndex++
		}
	}

	tools := NewToolCallTracker()
	var thinkBuf, textBuf strings.Builder

	out, streamErr := readStream(ctx, resp, func(ev Event) error {
		if rsn, ok := ev.ParseReasoningDelta(); ok {
			thinkBuf.WriteString(rsn)
			closeText()
			if !thinkingBlockOpen {
				sendClaudeEvent(w, flusher, canFlush, "content_block_start", map[string]any{
					"type":          "content_block_start",
					"index":         blockIndex,
					"content_block": map[string]any{"type": "thinking", "thinking": ""},
				})
				thinkingBlockOpen = true
			}
			sendClaudeEvent(w, flusher, canFlush, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "thinking_delta", "thinking": rsn},
			})
		}

		if txt, ok := ev.ParseTextDelta(); ok {
			textBuf.WriteString(txt)
			closeThinking()
			if !textBlockOpen {
				sendClaudeEvent(w, flusher, canFlush, "content_block_start", map[string]any{
					"type":          "content_block_start",
					"index":         blockIndex,
					"content_block": map[string]any{"type": "text", "text": ""},
				})
				textBlockOpen = true
			}
			sendClaudeEvent(w, flusher, canFlush, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "text_delta", "text": txt},
			})
		}

		if ev.Type == "response.output_item.added" {
			var itemWrap struct {
				Item ResponsesOutputItem `json:"item"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &itemWrap); err == nil && itemWrap.Item.Type == "function_call" {
				closeText()
				closeThinking()
				callID := itemWrap.Item.CallID
				if callID == "" {
					callID = itemWrap.Item.ID
				}
				tools.IndexFor(callID)
				tools.SetName(callID, itemWrap.Item.Name)
				openToolBlocks[callID] = blockIndex
				sendClaudeEvent(w, flusher, canFlush, "content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": blockIndex,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    callID,
						"name":  itemWrap.Item.Name,
						"input": map[string]any{},
					},
				})
				blockIndex++
			}
		}

		if callID, delta, ok := ev.ParseFunctionCallArgumentsDelta(); ok {
			tools.AppendArgs(callID, delta)
			idx, open := openToolBlocks[callID]
			if !open {
				closeText()
				closeThinking()
				tools.IndexFor(callID)
				idx = blockIndex
				openToolBlocks[callID] = idx
				sendClaudeEvent(w, flusher, canFlush, "content_block_start", map[string]any{
					"type":  "content_block_start",
					"index": idx,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    callID,
						"name":  tools.Name(callID),
						"input": map[string]any{},
					},
				})
				blockIndex++
			}
			sendClaudeEvent(w, flusher, canFlush, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": delta},
			})
		}
		return nil
	})

	if streamErr != nil {
		return thinkBuf.String(), textBuf.String(), streamErr
	}

	closeText()
	// Tool blocks stay open across interleaved deltas; close them in first-seen
	// order so clients see well-formed content blocks.
	for _, callID := range tools.Order() {
		idx, open := openToolBlocks[callID]
		if !open {
			continue
		}
		sendClaudeEvent(w, flusher, canFlush, "content_block_stop", map[string]any{
			"type": "content_block_stop", "index": idx,
		})
	}

	stopReason := "end_turn"
	if tools.Len() > 0 {
		stopReason = "tool_use"
	} else if out.SawTerm && out.Terminal.Type == "response.incomplete" {
		stopReason = "max_tokens"
	}

	sendClaudeEvent(w, flusher, canFlush, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"input_tokens":  out.Terminal.InputTokens,
			"output_tokens": out.Terminal.OutputTokens,
		},
	})
	sendClaudeEvent(w, flusher, canFlush, "message_stop", map[string]any{
		"type": "message_stop",
	})

	return thinkBuf.String(), textBuf.String(), nil
}

func StreamGeminiContent(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	resp, err := prepareCodexStream(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close stream body: %v", cerr)
		}
	}()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)

	emitGemini := func(parts []any, finish string) {
		candidate := map[string]any{
			"content": map[string]any{"role": "model", "parts": parts},
			"index":   0,
		}
		if finish != "" {
			candidate["finishReason"] = finish
		}
		emitSSEJSON(w, map[string]any{"candidates": []any{candidate}})
		if canFlush {
			flusher.Flush()
		}
	}

	tools := NewToolCallTracker()
	emittedCalls := make(map[string]bool)
	var thinkBuf, textBuf strings.Builder

	emitToolCall := func(callID string) {
		if emittedCalls[callID] {
			return
		}
		emittedCalls[callID] = true
		args := map[string]any{}
		if raw := strings.TrimSpace(tools.Args(callID)); raw != "" {
			_ = json.Unmarshal([]byte(raw), &args)
		}
		emitGemini([]any{map[string]any{
			"functionCall": map[string]any{
				"name": tools.Name(callID),
				"args": args,
			},
		}}, "")
	}

	out, streamErr := readStream(ctx, resp, func(ev Event) error {
		if rsn, ok := ev.ParseReasoningDelta(); ok {
			thinkBuf.WriteString(rsn)
			emitGemini([]any{map[string]any{"text": rsn, "thought": true}}, "")
		}
		if txt, ok := ev.ParseTextDelta(); ok {
			textBuf.WriteString(txt)
			emitGemini([]any{map[string]any{"text": txt}}, "")
		}

		if ev.Type == "response.output_item.added" || ev.Type == "response.output_item.done" {
			var itemWrap struct {
				Item ResponsesOutputItem `json:"item"`
			}
			if err := json.Unmarshal([]byte(ev.Data), &itemWrap); err == nil && itemWrap.Item.Type == "function_call" {
				callID := itemWrap.Item.CallID
				if callID == "" {
					callID = itemWrap.Item.ID
				}
				tools.IndexFor(callID)
				tools.SetName(callID, itemWrap.Item.Name)
				if ev.Type == "response.output_item.done" {
					if itemWrap.Item.Arguments != "" && tools.Args(callID) == "" {
						tools.AppendArgs(callID, itemWrap.Item.Arguments)
					}
					emitToolCall(callID)
				}
			}
		}

		if callID, delta, ok := ev.ParseFunctionCallArgumentsDelta(); ok {
			tools.IndexFor(callID)
			tools.AppendArgs(callID, delta)
		}
		return nil
	})

	if streamErr != nil {
		return thinkBuf.String(), textBuf.String(), streamErr
	}

	for _, callID := range tools.Order() {
		emitToolCall(callID)
	}

	finish := "STOP"
	if tools.Len() > 0 {
		finish = "TOOL_CALLS"
	} else if out.SawTerm && out.Terminal.Type == "response.incomplete" {
		finish = "MAX_TOKENS"
	}
	emitGemini([]any{map[string]any{"text": ""}}, finish)

	return thinkBuf.String(), textBuf.String(), nil
}

func sendClaudeEvent(w http.ResponseWriter, flusher http.Flusher, canFlush bool, event string, data any) {
	b, err := json.Marshal(data)
	if err == nil {
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
		if canFlush {
			flusher.Flush()
		}
	}
}

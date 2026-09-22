package geminiweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/promptcompat"
)

// StreamOpenAIChat relays a streamed Gemini turn as OpenAI chat chunks.
//
// It returns the accumulated thinking and text so the caller can record history
// after the stream has actually finished; writing the turn beforehand would log
// a successful reply even when the upstream stream failed mid-way.
func StreamOpenAIChat(ctx context.Context, accountID string, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	reader, err := prepareStream(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = reader.Close() }()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	id := fmt.Sprintf("chatcmpl-gemini-%d", time.Now().UnixNano())
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

	var thinkBuf, textBuf strings.Builder
	emitChunk(map[string]any{"role": "assistant"}, nil)
	for {
		chunk, readErr := reader.ReadChunk()
		if readErr != nil {
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return thinkBuf.String(), textBuf.String(), readErr
			}
			break
		}
		if chunk == nil {
			continue
		}
		if chunk.ThoughtDelta != "" {
			thinkBuf.WriteString(chunk.ThoughtDelta)
			emitChunk(map[string]any{"reasoning_content": chunk.ThoughtDelta}, nil)
		}
		if chunk.TextDelta != "" {
			textBuf.WriteString(chunk.TextDelta)
			emitChunk(map[string]any{"content": chunk.TextDelta}, nil)
		}
		if chunk.IsFinished {
			break
		}
	}

	if thinkBuf.Len() == 0 && textBuf.Len() == 0 {
		return "", "", upstreamEmptyError(accountID, reader)
	}

	emitChunk(map[string]any{}, "stop")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
	return thinkBuf.String(), textBuf.String(), nil
}

// StreamGeminiContent relays a streamed Gemini turn as Gemini content chunks,
// returning the accumulated thinking and text for the caller to record.
func StreamGeminiContent(ctx context.Context, accountID string, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	reader, err := prepareStream(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = reader.Close() }()

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

	var thinkBuf, textBuf strings.Builder
	for {
		chunk, readErr := reader.ReadChunk()
		if readErr != nil {
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return thinkBuf.String(), textBuf.String(), readErr
			}
			break
		}
		if chunk == nil {
			continue
		}
		if chunk.ThoughtDelta != "" {
			thinkBuf.WriteString(chunk.ThoughtDelta)
			emitGemini([]any{map[string]any{"text": chunk.ThoughtDelta, "thought": true}}, "")
		}
		if chunk.TextDelta != "" {
			textBuf.WriteString(chunk.TextDelta)
			emitGemini([]any{map[string]any{"text": chunk.TextDelta}}, "")
		}
		if chunk.IsFinished {
			break
		}
	}

	if thinkBuf.Len() == 0 && textBuf.Len() == 0 {
		return "", "", upstreamEmptyError(accountID, reader)
	}

	emitGemini([]any{map[string]any{"text": ""}}, "STOP")
	return thinkBuf.String(), textBuf.String(), nil
}

// StreamClaudeMessages relays a streamed Gemini turn as Claude message events,
// returning the accumulated thinking and text for the caller to record.
func StreamClaudeMessages(ctx context.Context, accountID string, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	reader, err := prepareStream(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = reader.Close() }()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	sendClaudeEvent(w, flusher, canFlush, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": msgID, "type": "message", "role": "assistant", "model": stdReq.ResponseModel,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": len(stdReq.PromptTokenText)/4 + 1, "output_tokens": 1},
		},
	})
	blockIndex := 0
	thinkingBlockOpen := false
	textBlockOpen := false
	outputTokens := 1

	var thinkBuf, textBuf strings.Builder
	for {
		chunk, readErr := reader.ReadChunk()
		if readErr != nil {
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return thinkBuf.String(), textBuf.String(), readErr
			}
			break
		}
		if chunk == nil {
			continue
		}

		if chunk.ThoughtDelta != "" {
			thinkBuf.WriteString(chunk.ThoughtDelta)
			if !thinkingBlockOpen {
				sendClaudeEvent(w, flusher, canFlush, "content_block_start", map[string]any{
					"type":          "content_block_start",
					"index":         blockIndex,
					"content_block": map[string]any{"type": "thinking", "thinking": ""},
				})
				thinkingBlockOpen = true
			}
			outputTokens += len(chunk.ThoughtDelta)/4 + 1
			sendClaudeEvent(w, flusher, canFlush, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "thinking_delta", "thinking": chunk.ThoughtDelta},
			})
		}

		if chunk.TextDelta != "" {
			textBuf.WriteString(chunk.TextDelta)
			if thinkingBlockOpen {
				sendClaudeEvent(w, flusher, canFlush, "content_block_stop", map[string]any{
					"type":  "content_block_stop",
					"index": blockIndex,
				})
				thinkingBlockOpen = false
				blockIndex++
			}
			if !textBlockOpen {
				sendClaudeEvent(w, flusher, canFlush, "content_block_start", map[string]any{
					"type":          "content_block_start",
					"index":         blockIndex,
					"content_block": map[string]any{"type": "text", "text": ""},
				})
				textBlockOpen = true
			}
			outputTokens += len(chunk.TextDelta)/4 + 1
			sendClaudeEvent(w, flusher, canFlush, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "text_delta", "text": chunk.TextDelta},
			})
		}

		if chunk.IsFinished {
			break
		}
	}

	if thinkingBlockOpen {
		sendClaudeEvent(w, flusher, canFlush, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": blockIndex,
		})
	}
	if textBlockOpen {
		sendClaudeEvent(w, flusher, canFlush, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": blockIndex,
		})
	}

	sendClaudeEvent(w, flusher, canFlush, "message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]any{"output_tokens": outputTokens},
	})

	if thinkBuf.Len() == 0 && textBuf.Len() == 0 {
		return "", "", upstreamEmptyError(accountID, reader)
	}

	sendClaudeEvent(w, flusher, canFlush, "message_stop", map[string]any{
		"type": "message_stop",
	})
	return thinkBuf.String(), textBuf.String(), nil
}

func prepareStream(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (*StreamReader, error) {
	prompt := stdReq.PromptTokenText
	if prompt == "" {
		prompt = stdReq.FinalPrompt
	}
	opts := GenerateOptions{
		Model:    stdReq.ResolvedModel,
		Thinking: stdReq.Thinking,
		FileIDs:  stdReq.RefFileIDs,
	}
	if opts.Model == "" {
		opts.Model = stdReq.RequestedModel
	}
	return client.StreamGenerateWithRetry(ctx, prompt, opts)
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
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

func emitSSEJSON(w http.ResponseWriter, data any) {
	b, err := json.Marshal(data)
	if err == nil {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
	}
}

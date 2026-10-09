package geminiweb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

// openStreamBuffered opens StreamGenerate and reads the first chunk BEFORE writing
// any bytes down w, so that an initial 1095 throttle can still switch and retry on another account.
func openStreamBuffered(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (*StreamReader, *ParsedChunk, error) {
	reader, err := prepareStream(ctx, client, stdReq)
	if err != nil {
		return nil, nil, err
	}

	chunk, err := readFirstChunk(reader)
	if err != nil {
		if closeErr := reader.Close(); closeErr != nil {
			config.Logger.Warn("[geminiweb] close after failed pre-read", "error", closeErr)
		}
		return nil, nil, err
	}
	return reader, chunk, nil
}

// readFirstChunk fetches the first non-nil chunk and classifies empty or throttled errors.
func readFirstChunk(reader *StreamReader) (*ParsedChunk, error) {
	for {
		chunk, readErr := reader.ReadChunk()
		if readErr != nil {
			if reader.parser.LastErrorCode() == 1095 {
				return nil, ErrGeminiThrottled
			}
			if errors.Is(readErr, io.EOF) {
				return nil, errors.New("gemini upstream returned empty output (check proxy/cookies)")
			}
			return nil, readErr
		}
		if chunk == nil {
			continue
		}
		if reader.parser.LastErrorCode() == 1095 && !chunk.IsFinished {
			return nil, ErrGeminiThrottled
		}
		return chunk, nil
	}
}

// StreamOpenAIChatBuffered relays a streamed Gemini turn as OpenAI chat chunks,
// buffering the first chunk so that an initial throttle failure can retry cleanly.
func StreamOpenAIChatBuffered(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	reader, firstChunk, err := openStreamBuffered(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			config.Logger.Warn("[geminiweb] stream close failed", "error", closeErr)
		}
	}()

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

	writeChunk := func(chunk *ParsedChunk) {
		if chunk.ThoughtDelta != "" {
			thinkBuf.WriteString(chunk.ThoughtDelta)
			emitChunk(map[string]any{"reasoning_content": chunk.ThoughtDelta}, nil)
		}
		if chunk.TextDelta != "" {
			textBuf.WriteString(chunk.TextDelta)
			emitChunk(map[string]any{"content": chunk.TextDelta}, nil)
		}
	}

	writeChunk(firstChunk)

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
		writeChunk(chunk)
		if chunk.IsFinished {
			break
		}
	}

	if reader.parser.LastErrorCode() == 1095 {
		config.Logger.Warn("[geminiweb] stream truncated by upstream throttle (1095)",
			"account", stdReq.ResponseModel, "thinking_len", thinkBuf.Len(), "text_len", textBuf.Len())
		return thinkBuf.String(), textBuf.String(),
			errors.New("gemini upstream throttled mid-stream (BardErrorInfo 1095); output truncated")
	}

	if thinkBuf.Len() == 0 && textBuf.Len() == 0 {
		return "", "", errors.New("gemini upstream returned empty output (check proxy/cookies)")
	}

	emitChunk(map[string]any{}, "stop")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
	return thinkBuf.String(), textBuf.String(), nil
}

// StreamGeminiContentBuffered relays a streamed Gemini turn as Gemini content chunks,
// buffering the first chunk so that an initial throttle failure can retry cleanly.
func StreamGeminiContentBuffered(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	reader, firstChunk, err := openStreamBuffered(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			config.Logger.Warn("[geminiweb] stream close failed", "error", closeErr)
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

	var thinkBuf, textBuf strings.Builder
	writeChunk := func(chunk *ParsedChunk) {
		if chunk.ThoughtDelta != "" {
			thinkBuf.WriteString(chunk.ThoughtDelta)
			emitGemini([]any{map[string]any{"text": chunk.ThoughtDelta, "thought": true}}, "")
		}
		if chunk.TextDelta != "" {
			textBuf.WriteString(chunk.TextDelta)
			emitGemini([]any{map[string]any{"text": chunk.TextDelta}}, "")
		}
	}

	writeChunk(firstChunk)

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
		writeChunk(chunk)
		if chunk.IsFinished {
			break
		}
	}

	if reader.parser.LastErrorCode() == 1095 {
		config.Logger.Warn("[geminiweb] stream truncated by upstream throttle (1095)",
			"account", stdReq.ResponseModel, "thinking_len", thinkBuf.Len(), "text_len", textBuf.Len())
		return thinkBuf.String(), textBuf.String(),
			errors.New("gemini upstream throttled mid-stream (BardErrorInfo 1095); output truncated")
	}

	if thinkBuf.Len() == 0 && textBuf.Len() == 0 {
		return "", "", errors.New("gemini upstream returned empty output (check proxy/cookies)")
	}

	emitGemini([]any{map[string]any{"text": ""}}, "STOP")
	return thinkBuf.String(), textBuf.String(), nil
}

// StreamClaudeMessagesBuffered relays a streamed Gemini turn as Claude message events,
// buffering the first chunk so that an initial throttle failure can retry cleanly.
func StreamClaudeMessagesBuffered(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	reader, firstChunk, err := openStreamBuffered(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			config.Logger.Warn("[geminiweb] stream close failed", "error", closeErr)
		}
	}()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())

	inputTokens := len(buildGeminiPrompt(stdReq))/4 + 1
	sendClaudeEvent(w, flusher, canFlush, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": msgID, "type": "message", "role": "assistant", "model": stdReq.ResponseModel,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": inputTokens, "output_tokens": 1},
		},
	})
	blockIndex := 0
	thinkingBlockOpen := false
	textBlockOpen := false
	outputTokens := 1

	var thinkBuf, textBuf strings.Builder
	writeChunk := func(chunk *ParsedChunk) {
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
	}

	writeChunk(firstChunk)

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
		writeChunk(chunk)
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

	if reader.parser.LastErrorCode() == 1095 {
		config.Logger.Warn("[geminiweb] stream truncated by upstream throttle (1095)",
			"account", stdReq.ResponseModel, "thinking_len", thinkBuf.Len(), "text_len", textBuf.Len())
		return thinkBuf.String(), textBuf.String(),
			errors.New("gemini upstream throttled mid-stream (BardErrorInfo 1095); output truncated")
	}

	if thinkBuf.Len() == 0 && textBuf.Len() == 0 {
		return "", "", errors.New("gemini upstream returned empty output (check proxy/cookies)")
	}

	sendClaudeEvent(w, flusher, canFlush, "message_stop", map[string]any{
		"type": "message_stop",
	})
	return thinkBuf.String(), textBuf.String(), nil
}

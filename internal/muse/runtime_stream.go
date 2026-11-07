package muse

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"ds2api/internal/assistantturn"
	"ds2api/internal/promptcompat"
	"ds2api/internal/toolcall"
)

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func StreamOpenAIChat(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	turn, err := StreamOpenAIChatWithTurn(ctx, client, req, w)
	return turn.Thinking, turn.Text, err
}

func StreamOpenAIChatWithTurn(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (assistantturn.Turn, error) {
	s := &streamWriter{w: w}
	id := fmt.Sprintf("chatcmpl-muse-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	chunk := func(delta map[string]any, finish any, usage any) {
		if !s.started {
			s.send("", map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": req.ResponseModel,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}}})
		}
		payload := map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": req.ResponseModel,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
		if usage != nil {
			payload["usage"] = usage
		}
		s.send("", payload)
	}
	turn, err := client.StreamChat(ctx, req, func(text, thinking string) error {
		if thinking != "" {
			chunk(map[string]any{"reasoning_content": thinking}, nil, nil)
		}
		if text != "" {
			chunk(map[string]any{"content": text}, nil, nil)
		}
		return s.err
	})
	if err != nil {
		if s.started {
			s.send("", map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
		}
		return turn, err
	}
	if len(turn.ToolCalls) > 0 {
		calls := toolcall.FormatOpenAIToolCalls(turn.ToolCalls, req.ToolsRaw)
		for i, call := range calls {
			call["index"] = i
		}
		chunk(map[string]any{"tool_calls": calls}, nil, nil)
	}
	chunk(map[string]any{}, assistantturn.FinishReason(turn), assistantturn.OpenAIChatUsage(turn))
	return turn, s.done()
}

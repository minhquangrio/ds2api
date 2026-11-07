package muse

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"ds2api/internal/assistantturn"
	claudefmt "ds2api/internal/format/claude"
	"ds2api/internal/promptcompat"
)

func StreamClaudeMessages(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	turn, err := StreamClaudeMessagesWithTurn(ctx, client, req, w)
	return turn.Thinking, turn.Text, err
}

func StreamClaudeMessagesWithTurn(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (assistantturn.Turn, error) {
	s := &streamWriter{w: w}
	id := fmt.Sprintf("msg_muse_%d", time.Now().UnixNano())
	start := func() {
		if !s.started {
			s.send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
				"id": id, "type": "message", "role": "assistant", "model": req.ResponseModel, "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
				"usage": map[string]any{"input_tokens": 0, "output_tokens": 0}}})
		}
	}
	index, openType := 0, ""
	closeBlock := func() {
		if openType != "" {
			s.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
			index++
			openType = ""
		}
	}
	emit := func(kind, text string) {
		if text == "" {
			return
		}
		start()
		if openType != kind {
			closeBlock()
			s.send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": kind, kind: ""}})
			openType = kind
		}
		s.send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": kind + "_delta", kind: text}})
	}
	turn, err := client.StreamChat(ctx, req, func(text, thinking string) error { emit("thinking", thinking); emit("text", text); return s.err })
	if err != nil {
		if s.started {
			s.send("error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": err.Error()}})
		}
		return turn, err
	}
	start()
	closeBlock()
	response := claudefmt.BuildMessageResponseFromTurn(id, req.ResponseModel, turn, req.Thinking)
	if len(turn.ToolCalls) > 0 {
		blocks, _ := response["content"].([]map[string]any)
		for _, block := range blocks {
			s.send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": block})
			s.send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
			index++
		}
	}
	s.send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": response["stop_reason"], "stop_sequence": nil}, "usage": response["usage"]})
	s.send("message_stop", map[string]any{"type": "message_stop"})
	return turn, s.err
}

package muse

import (
	"context"
	"net/http"

	"ds2api/internal/assistantturn"
	"ds2api/internal/promptcompat"
)

func StreamGeminiContent(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	turn, err := StreamGeminiContentWithTurn(ctx, client, req, w)
	return turn.Thinking, turn.Text, err
}

func StreamGeminiContentWithTurn(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (assistantturn.Turn, error) {
	s := &streamWriter{w: w}
	emit := func(parts []any, finish string, usage any) {
		candidate := map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "index": 0}
		if finish != "" {
			candidate["finishReason"] = finish
		}
		payload := map[string]any{"candidates": []any{candidate}, "modelVersion": req.ResponseModel}
		if usage != nil {
			payload["usageMetadata"] = usage
		}
		s.send("", payload)
	}
	turn, err := client.StreamChat(ctx, req, func(text, thinking string) error {
		if thinking != "" {
			emit([]any{map[string]any{"text": thinking, "thought": true}}, "", nil)
		}
		if text != "" {
			emit([]any{map[string]any{"text": text}}, "", nil)
		}
		return s.err
	})
	if err != nil {
		if s.started {
			s.send("", map[string]any{"error": map[string]any{"message": err.Error(), "status": "UNAVAILABLE"}})
		}
		return turn, err
	}
	parts := []any{}
	if len(turn.ToolCalls) > 0 {
		if turn.Thinking != "" {
			parts = append(parts, map[string]any{"text": turn.Thinking, "thought": true})
		}
		for _, call := range turn.ToolCalls {
			parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": call.Input}})
		}
	}
	finish := "STOP"
	if turn.ContentFilter {
		finish = "SAFETY"
	}
	emit(parts, finish, map[string]any{"promptTokenCount": turn.Usage.InputTokens, "candidatesTokenCount": turn.Usage.OutputTokens, "totalTokenCount": turn.Usage.TotalTokens})
	return turn, s.err
}

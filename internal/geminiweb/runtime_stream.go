package geminiweb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"ds2api/internal/promptcompat"
)

func prepareStream(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (*StreamReader, error) {
	prompt := buildGeminiPrompt(stdReq)
	opts := GenerateOptions{
		Model:    stdReq.ResolvedModel,
		Thinking: stdReq.Thinking,
		FileIDs:  stdReq.RefFileIDs,
	}
	if opts.Model == "" {
		opts.Model = stdReq.RequestedModel
	}
	return client.StreamGenerate(ctx, prompt, opts)
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

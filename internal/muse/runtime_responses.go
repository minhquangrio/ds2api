package muse

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"ds2api/internal/assistantturn"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/promptcompat"
)

func StreamResponsesAPI(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) error {
	_, err := StreamResponsesAPIWithTurn(ctx, client, req, w)
	return err
}

func StreamResponsesAPIWithTurn(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (assistantturn.Turn, error) {
	turn, _, err := StreamResponsesWithID(ctx, client, req, w, fmt.Sprintf("resp_muse_%d", time.Now().UnixNano()))
	return turn, err
}

// StreamResponsesWithID returns the exact completed object sent to the client,
// so response retrieval and previous_response_id use the same IDs and items.
func StreamResponsesWithID(ctx context.Context, client *Client, req promptcompat.StandardRequest, w http.ResponseWriter, id string) (assistantturn.Turn, map[string]any, error) {
	s := &streamWriter{w: w}
	messageID := "msg_" + id
	partAdded := false
	start := func() {
		if !s.started {
			response := map[string]any{"id": id, "object": "response", "status": "in_progress", "model": req.ResponseModel, "output": []any{}}
			s.send("response.created", map[string]any{"type": "response.created", "response": response})
		}
	}
	turn, err := client.StreamChat(ctx, req, func(text, _ string) error {
		if text == "" {
			return nil
		}
		start()
		if !partAdded {
			item := map[string]any{"id": messageID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}
			s.send("response.output_item.added", openaifmt.BuildResponsesOutputItemAddedPayload(id, messageID, 0, item))
			s.send("response.content_part.added", openaifmt.BuildResponsesContentPartAddedPayload(id, messageID, 0, 0, map[string]any{"type": "output_text", "text": "", "annotations": []any{}}))
			partAdded = true
		}
		s.send("response.output_text.delta", openaifmt.BuildResponsesTextDeltaPayload(id, messageID, 0, 0, text))
		return s.err
	})
	if err != nil {
		if s.started {
			status, code := ErrorDetail(turn, err)
			failed := map[string]any{"id": id, "object": "response", "status": "failed", "model": req.ResponseModel,
				"output": []any{}, "error": map[string]any{"code": code, "message": err.Error(), "status": status}}
			s.send("response.failed", map[string]any{"type": "response.failed", "response": failed})
		}
		return turn, nil, err
	}
	start()
	obj := openaifmt.BuildResponseObjectWithToolCalls(id, req.ResponseModel, turn.Prompt, turn.Thinking, turn.Text, turn.ToolCalls, req.ToolsRaw)
	obj["usage"] = assistantturn.OpenAIResponsesUsage(turn)
	items, _ := obj["output"].([]any)
	// The text item streamed at index zero must keep that ID/index in completed.
	if partAdded {
		for i, value := range items {
			item := value.(map[string]any)
			if item["type"] == "message" {
				item["id"] = messageID
				items[0], items[i] = items[i], items[0]
				break
			}
		}
	}
	for index, value := range items {
		item := value.(map[string]any)
		itemID, _ := item["id"].(string)
		if item["type"] == "message" && partAdded {
			part := map[string]any{"type": "output_text", "text": turn.Text, "annotations": []any{}}
			item["content"] = []any{part}
			s.send("response.output_text.done", openaifmt.BuildResponsesTextDonePayload(id, itemID, index, 0, turn.Text))
			s.send("response.content_part.done", openaifmt.BuildResponsesContentPartDonePayload(id, itemID, index, 0, part))
		} else {
			s.send("response.output_item.added", openaifmt.BuildResponsesOutputItemAddedPayload(id, itemID, index, item))
			if item["type"] == "function_call" {
				callID, _ := item["call_id"].(string)
				name, _ := item["name"].(string)
				args, _ := item["arguments"].(string)
				s.send("response.function_call_arguments.delta", openaifmt.BuildResponsesFunctionCallArgumentsDeltaPayload(id, itemID, index, callID, args))
				s.send("response.function_call_arguments.done", openaifmt.BuildResponsesFunctionCallArgumentsDonePayload(id, itemID, index, callID, name, args))
			}
		}
		s.send("response.output_item.done", openaifmt.BuildResponsesOutputItemDonePayload(id, itemID, index, item))
	}
	s.send("response.completed", openaifmt.BuildResponsesCompletedPayload(obj))
	return turn, obj, s.err
}

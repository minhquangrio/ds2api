package codex

import (
	"testing"

	"ds2api/internal/promptcompat"
)

func TestBuildResponsesBody(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "gpt-6-luna",
		PromptMessages: []map[string]any{
			{"role": "system", "content": "You are a helpful coding assistant."},
			{"role": "user", "content": "Hello world"},
		},
		ToolsRaw: []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search tool",
					"parameters": map[string]any{
						"type": "object",
					},
				},
			},
		},
	}

	body, err := BuildResponsesBody(stdReq)
	if err != nil {
		t.Fatalf("BuildResponsesBody failed: %v", err)
	}

	if body["model"] != "gpt-6-luna" {
		t.Errorf("model = %v; want gpt-6-luna", body["model"])
	}
	if body["instructions"] != "You are a helpful coding assistant." {
		t.Errorf("instructions mismatch: %v", body["instructions"])
	}

	input, ok := body["input"].([]map[string]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected 1 input item, got %v", body["input"])
	}
	if input[0]["role"] != "user" {
		t.Errorf("input role = %v; want user", input[0]["role"])
	}

	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", body["tools"])
	}
	if tools[0]["name"] != "search" {
		t.Errorf("tool name = %v; want search", tools[0]["name"])
	}
}

// TestBuildResponsesBodyModelFallback ensures an empty request model falls back
// to the configured default rather than being forwarded upstream as "".
func TestBuildResponsesBodyModelFallback(t *testing.T) {
	body, err := BuildResponsesBodyWithDefault(promptcompat.StandardRequest{}, "configured-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["model"] != "configured-model" {
		t.Errorf("model = %v; want configured-model", body["model"])
	}

	// With no configured default either, the upstream must still receive a name.
	body, err = BuildResponsesBodyWithDefault(promptcompat.StandardRequest{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["model"] != DefaultFallbackModel {
		t.Errorf("model = %v; want %s", body["model"], DefaultFallbackModel)
	}
}

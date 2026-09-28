package gemini

import (
	"strings"
	"testing"
)

func TestNormalizeGeminiRequestNoThinkingModelForcesThinkingOff(t *testing.T) {
	req := map[string]any{
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "hello"}},
			},
		},
		"reasoning_effort": "high",
	}
	out, err := normalizeGeminiRequest(testGeminiConfig{}, "gemini-2.5-pro-nothinking", req, false)
	if err != nil {
		t.Fatalf("normalizeGeminiRequest error: %v", err)
	}
	if out.ResolvedModel != "gemini-pro-nothinking" {
		t.Fatalf("resolved model mismatch: got=%q", out.ResolvedModel)
	}
	if out.Thinking {
		t.Fatalf("expected nothinking model to force thinking off")
	}
	if out.Search {
		t.Fatalf("expected search=false, got=%v", out.Search)
	}
}

func TestNormalizeGeminiRequestPopulatesPromptMessagesWithTools(t *testing.T) {
	req := map[string]any{
		"contents": []any{
			map[string]any{
				"role":  "user",
				"parts": []any{map[string]any{"text": "search the web"}},
			},
		},
		"tools": []any{
			map[string]any{
				"functionDeclarations": []any{
					map[string]any{
						"name":        "search_web",
						"description": "Performs web search",
					},
				},
			},
		},
	}
	out, err := normalizeGeminiRequest(testGeminiConfig{}, "gemini-2.5-pro", req, false)
	if err != nil {
		t.Fatalf("normalizeGeminiRequest error: %v", err)
	}
	if len(out.PromptMessages) == 0 {
		t.Fatalf("expected non-empty PromptMessages")
	}
	foundTool := false
	for _, m := range out.PromptMessages {
		if content, _ := m["content"].(string); strings.Contains(content, "search_web") {
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("expected tool prompt inside PromptMessages, got: %#v", out.PromptMessages)
	}
}

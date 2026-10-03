package claudeconv

import (
	"testing"
)

type mockAliasReader map[string]string

func (m mockAliasReader) ModelAliases() map[string]string {
	return m
}

func TestFlattenSystemPromptString(t *testing.T) {
	input := "You are an expert engineer."
	got := FlattenSystemPrompt(input)
	if got != input {
		t.Fatalf("expected %q, got %q", input, got)
	}
}

func TestFlattenSystemPromptTextBlocksJoinsWithNewline(t *testing.T) {
	input := []any{
		map[string]any{"type": "text", "text": "line1"},
		map[string]any{"type": "text", "text": "line2"},
		map[string]any{"type": "text", "text": "line3"},
	}
	expected := "line1\nline2\nline3"
	got := FlattenSystemPrompt(input)
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestFlattenSystemPromptStripsCacheControlAndIgnoresNonTextBlocks(t *testing.T) {
	input := []any{
		map[string]any{
			"type": "text",
			"text": "instruction with cache",
			"cache_control": map[string]any{
				"type": "ephemeral",
			},
		},
		map[string]any{
			"type": "image",
			"source": map[string]any{
				"type": "base64",
				"data": "xyz",
			},
		},
		map[string]any{
			"type": "text",
			"text": "final rule",
		},
	}
	expected := "instruction with cache\nfinal rule"
	got := FlattenSystemPrompt(input)
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestFlattenSystemPromptNilAndUnknownShapes(t *testing.T) {
	cases := []any{
		nil,
		12345,
		true,
		map[string]any{"text": "not an array or string"},
		[]any{},
		[]any{"invalid block type", 42},
		[]any{map[string]any{"type": "unknown", "text": "should be ignored"}},
	}

	for _, tc := range cases {
		got := FlattenSystemPrompt(tc)
		if got != "" {
			t.Fatalf("expected empty string for %#v, got %q", tc, got)
		}
	}
}

func TestConvertClaudeToDeepSeekArraySystemProducesSystemMessage(t *testing.T) {
	req := map[string]any{
		"model": "claude-sonnet-4-5",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "first prompt",
				"cache_control": map[string]any{
					"type": "ephemeral",
				},
			},
			map[string]any{
				"type": "text",
				"text": "second prompt",
			},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}

	res := ConvertClaudeToDeepSeek(req, mockAliasReader{}, "claude-sonnet-4-5")
	messages, ok := res["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %#v", res["messages"])
	}

	sysMsg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("expected message 0 to be map[string]any, got %#v", messages[0])
	}
	if sysMsg["role"] != "system" {
		t.Fatalf("expected role system, got %v", sysMsg["role"])
	}
	expectedContent := "first prompt\nsecond prompt"
	if sysMsg["content"] != expectedContent {
		t.Fatalf("expected content %q, got %q", expectedContent, sysMsg["content"])
	}
}

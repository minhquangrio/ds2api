package prompt

import (
	"strings"
	"testing"
)

func TestRenderMessagesGeminiWebSingleUser(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Xin chào, hãy giới thiệu bản thân trong một câu."},
	}
	got := RenderMessages(messages, StyleGeminiWeb)
	expected := "Xin chào, hãy giới thiệu bản thân trong một câu."
	if got != expected {
		t.Fatalf("expected pure text without markers, got %q", got)
	}
	if strings.Contains(got, "<User>:") || strings.Contains(got, "<Assistant>:") {
		t.Fatalf("found unwanted pseudo-tags in single user prompt: %q", got)
	}
}

func TestRenderMessagesGeminiWebSystemAndUser(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "You are a helpful coding assistant."},
		{"role": "user", "content": "Write a hello world function."},
	}
	got := RenderMessages(messages, StyleGeminiWeb)

	if !strings.HasPrefix(got, "[Instructions]\nYou are a helpful coding assistant.") {
		t.Fatalf("expected [Instructions] prefix, got %q", got)
	}
	if !strings.Contains(got, "User: Write a hello world function.") {
		t.Fatalf("expected User: turn, got %q", got)
	}
	if strings.HasSuffix(got, "Model:") || strings.HasSuffix(got, "<Assistant>:") {
		t.Fatalf("prompt should not end with dangling bot marker: %q", got)
	}
}

func TestRenderMessagesGeminiWebMultiTurn(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Hello!"},
		{"role": "assistant", "content": "Hi! How can I help you?"},
		{"role": "user", "content": "Tell me a joke."},
	}
	got := RenderMessages(messages, StyleGeminiWeb)

	expected := "User: Hello!\n\nModel: Hi! How can I help you?\n\nUser: Tell me a joke."
	if got != expected {
		t.Fatalf("unexpected multi-turn rendering:\nexpected: %q\ngot:      %q", expected, got)
	}
	if strings.HasSuffix(got, "Model:") || strings.HasSuffix(got, "<Assistant>:") {
		t.Fatalf("prompt must not end with trailing bot marker: %q", got)
	}
}

func TestRenderMessagesGeminiWebToolResultAndReasoning(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "What is the weather?"},
		{"role": "assistant", "content": "[reasoning_content]checking weather[/reasoning_content]\nI will check the weather."},
		{"role": "tool", "content": `{"temperature": 25, "unit": "C"}`},
		{"role": "user", "content": "Now summarize it."},
	}
	got := RenderMessages(messages, StyleGeminiWeb)

	if !strings.Contains(got, "User: [Tool Result]\n{\"temperature\": 25, \"unit\": \"C\"}") {
		t.Fatalf("expected tool result formatted as user input, got %q", got)
	}
	if !strings.Contains(got, "Model: [reasoning_content]checking weather[/reasoning_content]\nI will check the weather.") {
		t.Fatalf("expected reasoning and assistant content inside Model turn, got %q", got)
	}
	if !strings.HasSuffix(got, "User: Now summarize it.") {
		t.Fatalf("expected prompt to end at last user message, got %q", got)
	}
}

func TestRenderMessagesGeminiWebPreservesLiteralMarkers(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Why does <User>: and <Assistant>: break the model?"},
	}
	got := RenderMessages(messages, StyleGeminiWeb)

	if got != "Why does <User>: and <Assistant>: break the model?" {
		t.Fatalf("literal markers in user message were mangled: %q", got)
	}
}

func TestRenderMessagesGeminiWebMarkdownImagesReplaced(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Check this ![chart](https://example.com/chart.png)"},
	}
	got := RenderMessages(messages, StyleGeminiWeb)

	if !strings.Contains(got, "[chart](https://example.com/chart.png)") {
		t.Fatalf("expected markdown image to be replaced by link, got %q", got)
	}
}

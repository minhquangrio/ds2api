package geminiweb

import (
	"strings"
	"testing"

	"ds2api/internal/promptcompat"
)

func TestBuildGeminiPromptWithPromptMessages(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		PromptMessages: []map[string]any{
			{"role": "system", "content": "Tool and system instructions."},
			{"role": "user", "content": "How to write Go code?"},
		},
		FinalPrompt: "<System>:Tool and system instructions.<User>:How to write Go code?<Assistant>:",
	}

	got := buildGeminiPrompt(stdReq)
	if !strings.HasPrefix(got, "[Instructions]\nTool and system instructions.") {
		t.Fatalf("expected [Instructions] prefix from PromptMessages, got %q", got)
	}
	if !strings.Contains(got, "User: How to write Go code?") {
		t.Fatalf("expected User: turn, got %q", got)
	}
	if strings.Contains(got, "<Assistant>:") || strings.Contains(got, "<User>:") {
		t.Fatalf("found DeepSeek tags in Gemini prompt: %q", got)
	}
}

func TestBuildGeminiPromptFallbackWhenPromptMessagesEmpty(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		PromptMessages: nil,
		FinalPrompt:    "<User>:Xin chào<Assistant>:",
	}

	got := buildGeminiPrompt(stdReq)
	if got != "Xin chào" {
		t.Fatalf("expected fallback to clean single user prompt to 'Xin chào', got %q", got)
	}
}

func TestCleanGeminiPromptFallbackSingleUser(t *testing.T) {
	input := "<User>:Hello world<Assistant>:"
	got := CleanGeminiPromptFallback(input)
	if got != "Hello world" {
		t.Fatalf("expected clean user message, got %q", got)
	}
}

func TestCleanGeminiPromptFallbackMultiTurn(t *testing.T) {
	input := "<System>:Be helpful.<User>:Hi<Assistant>:Hello!<User>:How are you?<Assistant>:"
	got := CleanGeminiPromptFallback(input)

	if strings.HasSuffix(got, "<Assistant>:") || strings.HasSuffix(got, "Model:") {
		t.Fatalf("expected trailing marker to be removed, got %q", got)
	}
	if !strings.HasPrefix(got, "[Instructions]\nBe helpful.") {
		t.Fatalf("expected [Instructions] header, got %q", got)
	}
	if !strings.Contains(got, "User: Hi") || !strings.Contains(got, "Model: Hello!") {
		t.Fatalf("expected transformed dialogue turns, got %q", got)
	}
}

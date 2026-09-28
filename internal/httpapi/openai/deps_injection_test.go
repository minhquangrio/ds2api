package openai

import (
	"strings"
	"testing"

	"ds2api/internal/promptcompat"
)

type mockOpenAIConfig struct {
	aliases             map[string]string
	autoDeleteMode      string
	toolMode            string
	earlyEmit           string
	responsesTTL        int
	embedProv           string
	currentInputEnabled bool
	currentInputMin     int
	thinkingInjection   *bool
	thinkingPrompt      string
	autoRouteVision     bool
}

func (m mockOpenAIConfig) ModelAliases() map[string]string     { return m.aliases }
func (m mockOpenAIConfig) ToolcallMode() string                { return m.toolMode }
func (m mockOpenAIConfig) ToolcallEarlyEmitConfidence() string { return m.earlyEmit }
func (m mockOpenAIConfig) ResponsesStoreTTLSeconds() int       { return m.responsesTTL }
func (m mockOpenAIConfig) EmbeddingsProvider() string          { return m.embedProv }
func (m mockOpenAIConfig) AutoDeleteMode() string {
	if m.autoDeleteMode == "" {
		return "none"
	}
	return m.autoDeleteMode
}
func (m mockOpenAIConfig) AutoDeleteSessions() bool      { return false }
func (m mockOpenAIConfig) CurrentInputFileEnabled() bool { return m.currentInputEnabled }
func (m mockOpenAIConfig) CurrentInputFileMinChars() int {
	return m.currentInputMin
}
func (m mockOpenAIConfig) ThinkingInjectionEnabled() bool {
	if m.thinkingInjection == nil {
		return false
	}
	return *m.thinkingInjection
}
func (m mockOpenAIConfig) ThinkingInjectionPrompt() string                          { return m.thinkingPrompt }
func (mockOpenAIConfig) ExpertPromptSegmentEnabled() bool                           { return false }
func (mockOpenAIConfig) ExpertPromptSegmentMaxChars() int                           { return 120000 }
func (mockOpenAIConfig) ExpertTextFileInlineEnabled() bool                          { return false }
func (mockOpenAIConfig) ExpertTextFileInlineMaxFileBytes() int                      { return 3 * 1024 * 1024 }
func (mockOpenAIConfig) ExpertTextFileInlineAllowedExtensions() map[string]struct{} { return nil }
func (m mockOpenAIConfig) AutoRouteVisionEnabled() bool                             { return m.autoRouteVision }

func TestNormalizeOpenAIChatRequestWithConfigInterface(t *testing.T) {
	cfg := mockOpenAIConfig{
		aliases: map[string]string{
			"my-model": "deepseek-v4-flash-search",
		},
	}
	req := map[string]any{
		"model":    "my-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	out, err := promptcompat.NormalizeOpenAIChatRequest(cfg, req, "")
	if err != nil {
		t.Fatalf("promptcompat.NormalizeOpenAIChatRequest error: %v", err)
	}
	if out.ResolvedModel != "deepseek-v4-flash-search" {
		t.Fatalf("resolved model mismatch: got=%q", out.ResolvedModel)
	}
	if !out.Search || !out.Thinking {
		t.Fatalf("unexpected model flags: thinking=%v search=%v", out.Thinking, out.Search)
	}
}

func TestNormalizeOpenAIChatRequestDisablesThinkingForNoThinkingModel(t *testing.T) {
	cfg := mockOpenAIConfig{}
	req := map[string]any{
		"model":            "deepseek-v4-pro-nothinking",
		"messages":         []any{map[string]any{"role": "user", "content": "hello"}},
		"reasoning_effort": "high",
	}
	out, err := promptcompat.NormalizeOpenAIChatRequest(cfg, req, "")
	if err != nil {
		t.Fatalf("promptcompat.NormalizeOpenAIChatRequest error: %v", err)
	}
	if out.ResolvedModel != "deepseek-v4-pro-nothinking" {
		t.Fatalf("resolved model mismatch: got=%q", out.ResolvedModel)
	}
	if out.Thinking {
		t.Fatalf("expected nothinking model to force thinking off")
	}
	if out.Search {
		t.Fatalf("expected search=false for deepseek-v4-pro-nothinking, got=%v", out.Search)
	}
}

func TestNormalizeOpenAIResponsesRequestAlwaysAcceptsWideInput(t *testing.T) {
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"input": "hi",
	}

	out, err := promptcompat.NormalizeOpenAIResponsesRequest(mockOpenAIConfig{
		aliases: map[string]string{},
	}, req, "")
	if err != nil {
		t.Fatalf("unexpected error for wide input request: %v", err)
	}
	if out.Surface != "openai_responses" {
		t.Fatalf("unexpected surface: %q", out.Surface)
	}
	if !strings.Contains(out.FinalPrompt, "<User>:hi") {
		t.Fatalf("unexpected final prompt: %q", out.FinalPrompt)
	}
}

func TestNormalizeOpenAIChatRequestPopulatesPromptMessagesWithTools(t *testing.T) {
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "run tool"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "calculator",
					"description": "Calculate math expressions",
				},
			},
		},
	}
	out, err := promptcompat.NormalizeOpenAIChatRequest(mockOpenAIConfig{}, req, "")
	if err != nil {
		t.Fatalf("NormalizeOpenAIChatRequest error: %v", err)
	}
	if len(out.PromptMessages) == 0 {
		t.Fatalf("expected non-empty PromptMessages")
	}
	found := false
	for _, m := range out.PromptMessages {
		if content, _ := m["content"].(string); strings.Contains(content, "calculator") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool prompt inside PromptMessages, got: %#v", out.PromptMessages)
	}
}

func TestNormalizeOpenAIResponsesRequestPopulatesPromptMessagesWithTools(t *testing.T) {
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"input": "calculate 2+2",
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "calculator",
				"description": "Calculate math expressions",
			},
		},
	}
	out, err := promptcompat.NormalizeOpenAIResponsesRequest(mockOpenAIConfig{}, req, "")
	if err != nil {
		t.Fatalf("NormalizeOpenAIResponsesRequest error: %v", err)
	}
	if len(out.PromptMessages) == 0 {
		t.Fatalf("expected non-empty PromptMessages")
	}
	found := false
	for _, m := range out.PromptMessages {
		if content, _ := m["content"].(string); strings.Contains(content, "calculator") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool prompt inside PromptMessages, got: %#v", out.PromptMessages)
	}
}

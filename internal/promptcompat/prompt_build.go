package promptcompat

import (
	"ds2api/internal/prompt"
)

func buildOpenAIFinalPrompt(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return BuildOpenAIPrompt(messagesRaw, toolsRaw, traceID, DefaultToolChoicePolicy(), thinkingEnabled)
}

func BuildOpenAIPrompt(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	return buildOpenAIPrompt(messagesRaw, toolsRaw, traceID, toolPolicy, thinkingEnabled, true)
}

func BuildOpenAIPromptWithToolInstructionsOnly(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	return buildOpenAIPrompt(messagesRaw, toolsRaw, traceID, toolPolicy, thinkingEnabled, false)
}

func BuildOpenAIPromptWithMessages(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool, includeToolDescriptions bool) ([]map[string]any, string, []string) {
	messages := NormalizeOpenAIMessagesForPrompt(messagesRaw, traceID)
	toolNames := []string{}
	if tools, ok := toolsRaw.([]any); ok && len(tools) > 0 {
		if includeToolDescriptions {
			messages, toolNames = injectToolPrompt(messages, tools, toolPolicy)
		} else {
			messages, toolNames = injectToolPromptInstructionsOnly(messages, tools, toolPolicy)
		}
	}
	return messages, prompt.MessagesPrepareWithThinking(messages, thinkingEnabled), toolNames
}

func buildOpenAIPrompt(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool, includeToolDescriptions bool) (string, []string) {
	_, finalPrompt, toolNames := BuildOpenAIPromptWithMessages(messagesRaw, toolsRaw, traceID, toolPolicy, thinkingEnabled, includeToolDescriptions)
	return finalPrompt, toolNames
}

// BuildOpenAIPromptForAdapter exposes the OpenAI-compatible prompt building flow so
// other protocol adapters (for example Gemini) can reuse the same tool/history
// normalization logic and remain behavior-compatible with chat/completions.
func BuildOpenAIPromptForAdapter(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return buildOpenAIFinalPrompt(messagesRaw, toolsRaw, traceID, thinkingEnabled)
}

func BuildOpenAIPromptForAdapterWithMessages(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) ([]map[string]any, string, []string) {
	return BuildOpenAIPromptWithMessages(messagesRaw, toolsRaw, traceID, DefaultToolChoicePolicy(), thinkingEnabled, true)
}

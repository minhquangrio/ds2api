package geminiweb

import (
	"strings"

	"ds2api/internal/prompt"
	"ds2api/internal/promptcompat"
)

// buildGeminiPrompt formats a StandardRequest into a prompt string suitable for Google Gemini Web.
// If PromptMessages is populated, it renders via prompt.StyleGeminiWeb.
// Otherwise, it sanitizes FinalPrompt using CleanGeminiPromptFallback.
func buildGeminiPrompt(stdReq promptcompat.StandardRequest) string {
	if len(stdReq.PromptMessages) > 0 {
		return prompt.RenderMessages(stdReq.PromptMessages, prompt.StyleGeminiWeb)
	}
	raw := stdReq.PromptTokenText
	if raw == "" {
		raw = stdReq.FinalPrompt
	}
	return CleanGeminiPromptFallback(raw)
}

// CleanGeminiPromptFallback sanitizes a DeepSeek-style prompt (<System>:<User>:<Assistant>:)
// for Gemini Web by stripping dangling bot markers and transforming role tags into natural dialogue.
func CleanGeminiPromptFallback(p string) string {
	s := strings.TrimSpace(p)
	if s == "" {
		return ""
	}
	// Strip trailing <Assistant>: marker that triggers Gemini safety refusals
	s = strings.TrimSuffix(s, "<Assistant>:")
	s = strings.TrimSpace(s)

	// If prompt is simply "<User>:content", extract content directly
	if strings.HasPrefix(s, "<User>:") && !strings.Contains(s, "<System>:") && !strings.Contains(s, "<Assistant>:") && !strings.Contains(s, "<Tool>:") {
		return strings.TrimSpace(strings.TrimPrefix(s, "<User>:"))
	}

	// Transform remaining role markers into clean text format
	s = strings.ReplaceAll(s, "<System>:", "[Instructions]\n")
	s = strings.ReplaceAll(s, "<User>:", "\n\nUser: ")
	s = strings.ReplaceAll(s, "<Assistant>:", "\n\nModel: ")
	s = strings.ReplaceAll(s, "<Tool>:", "\n\nUser: [Tool Result]\n")

	return strings.TrimSpace(s)
}

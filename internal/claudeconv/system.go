package claudeconv

import (
	"strings"
)

// FlattenSystemPrompt extracts plain text from an Anthropic top-level "system"
// value. Accepts a string, or an array of content blocks (only blocks whose
// "type" is "text" contribute their "text"; cache_control and other keys are
// ignored). Multiple text blocks are joined with "\n", matching the
// message-level join convention in normalizeClaudeMessages.
func FlattenSystemPrompt(system any) string {
	switch v := system.(type) {
	case string:
		return v
	case []any:
		var textParts []string
		for _, block := range v {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			tStr, _ := m["type"].(string)
			if !strings.EqualFold(strings.TrimSpace(tStr), "text") {
				continue
			}
			if text, ok := m["text"].(string); ok {
				textParts = append(textParts, text)
			}
		}
		return strings.Join(textParts, "\n")
	case []map[string]any:
		var textParts []string
		for _, m := range v {
			tStr, _ := m["type"].(string)
			if !strings.EqualFold(strings.TrimSpace(tStr), "text") {
				continue
			}
			if text, ok := m["text"].(string); ok {
				textParts = append(textParts, text)
			}
		}
		return strings.Join(textParts, "\n")
	default:
		return ""
	}
}

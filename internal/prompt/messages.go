package prompt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var markdownImagePattern = regexp.MustCompile(`!\[(.*?)\]\((.*?)\)`)

const (
	systemMarker    = "<System>:"
	userMarker      = "<User>:"
	assistantMarker = "<Assistant>:"
	toolMarker      = "<Tool>:"
)

type PromptRenderStyle int

const (
	StyleDeepSeekWeb PromptRenderStyle = iota // <System>:...<User>:...<Assistant>:
	StyleGeminiWeb                            // Clean format for Google Gemini Web
)

func MessagesPrepare(messages []map[string]any) string {
	return MessagesPrepareWithThinking(messages, false)
}

func MessagesPrepareWithThinking(messages []map[string]any, _ bool) string {
	return RenderMessages(messages, StyleDeepSeekWeb)
}

type promptBlock struct {
	Role string
	Text string
}

func RenderMessages(messages []map[string]any, style PromptRenderStyle) string {
	processed := make([]promptBlock, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		text := NormalizeContent(m["content"])
		processed = append(processed, promptBlock{Role: role, Text: text})
	}
	if len(processed) == 0 {
		return ""
	}
	merged := make([]promptBlock, 0, len(processed))
	for _, msg := range processed {
		if len(merged) > 0 && merged[len(merged)-1].Role == msg.Role {
			merged[len(merged)-1].Text += "\n\n" + msg.Text
			continue
		}
		merged = append(merged, msg)
	}

	var out string
	switch style {
	case StyleGeminiWeb:
		out = renderGeminiWeb(merged)
	default:
		out = renderDeepSeekWeb(merged)
	}

	return markdownImagePattern.ReplaceAllString(out, `[${1}](${2})`)
}

func renderDeepSeekWeb(merged []promptBlock) string {
	parts := make([]string, 0, len(merged)+1)
	lastRole := ""
	for _, m := range merged {
		lastRole = m.Role
		switch m.Role {
		case "assistant":
			parts = append(parts, formatRoleBlock(assistantMarker, m.Text))
		case "tool":
			if strings.TrimSpace(m.Text) != "" {
				parts = append(parts, formatRoleBlock(toolMarker, m.Text))
			}
		case "system":
			if text := strings.TrimSpace(m.Text); text != "" {
				parts = append(parts, formatRoleBlock(systemMarker, text))
			}
		case "user":
			parts = append(parts, formatRoleBlock(userMarker, m.Text))
		default:
			if strings.TrimSpace(m.Text) != "" {
				parts = append(parts, m.Text)
			}
		}
	}
	if lastRole != "assistant" {
		parts = append(parts, assistantMarker)
	}
	return strings.Join(parts, "")
}

func renderGeminiWeb(merged []promptBlock) string {
	var systemTexts []string
	var turns []promptBlock

	for _, m := range merged {
		if m.Role == "system" {
			if text := strings.TrimSpace(m.Text); text != "" {
				systemTexts = append(systemTexts, text)
			}
		} else {
			turns = append(turns, m)
		}
	}

	hasSystem := len(systemTexts) > 0

	if !hasSystem && len(turns) == 1 && turns[0].Role == "user" {
		return strings.TrimSpace(turns[0].Text)
	}

	var sections []string

	if hasSystem {
		sections = append(sections, "[Instructions]\n"+strings.Join(systemTexts, "\n\n"))
	}

	for _, turn := range turns {
		text := strings.TrimSpace(turn.Text)
		if text == "" {
			continue
		}
		switch turn.Role {
		case "user":
			sections = append(sections, "User: "+text)
		case "assistant":
			sections = append(sections, "Model: "+text)
		case "tool":
			sections = append(sections, "User: [Tool Result]\n"+text)
		default:
			sections = append(sections, text)
		}
	}

	return strings.Join(sections, "\n\n")
}

// formatRoleBlock produces a single concatenated block: marker + text.
func formatRoleBlock(marker, text string) string {
	return marker + text
}

func NormalizeContent(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typeStr, _ := m["type"].(string)
			typeStr = strings.ToLower(strings.TrimSpace(typeStr))
			if typeStr == "text" || typeStr == "output_text" || typeStr == "input_text" {
				if txt, ok := m["text"].(string); ok && txt != "" {
					parts = append(parts, txt)
					continue
				}
				if txt, ok := m["content"].(string); ok && txt != "" {
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

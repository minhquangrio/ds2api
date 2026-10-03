package codex

import (
	"encoding/json"
	"strings"

	"ds2api/internal/promptcompat"
)

func BuildInput(stdReq promptcompat.StandardRequest) (string, []map[string]any) {
	var instructionsBuilder strings.Builder
	var inputItems []map[string]any

	if len(stdReq.PromptMessages) > 0 {
		for _, msg := range stdReq.PromptMessages {
			role, _ := msg["role"].(string)

			switch role {
			case "system":
				content, _ := msg["content"].(string)
				if instructionsBuilder.Len() > 0 {
					instructionsBuilder.WriteString("\n\n")
				}
				instructionsBuilder.WriteString(content)

			case "user":
				parts := buildContentParts(msg["content"], "input_text")
				if len(parts) > 0 {
					inputItems = append(inputItems, map[string]any{
						"type":    "message",
						"role":    "user",
						"content": parts,
					})
				}

			case "assistant":
				// Handle tool calls
				if tcs, ok := msg["tool_calls"].([]any); ok && len(tcs) > 0 {
					for _, tcRaw := range tcs {
						tc, ok := tcRaw.(map[string]any)
						if !ok {
							continue
						}
						id, _ := tc["id"].(string)
						fn, _ := tc["function"].(map[string]any)
						fnName, _ := fn["name"].(string)
						fnArgs, _ := fn["arguments"].(string)
						if fnArgs == "" {
							if rawArgs, ok := fn["arguments"]; ok {
								if b, err := json.Marshal(rawArgs); err == nil {
									fnArgs = string(b)
								}
							}
						}

						inputItems = append(inputItems, map[string]any{
							"type":      "function_call",
							"call_id":   id,
							"name":      fnName,
							"arguments": fnArgs,
						})
					}
				}

				parts := buildContentParts(msg["content"], "text")
				if len(parts) > 0 {
					inputItems = append(inputItems, map[string]any{
						"type":    "message",
						"role":    "assistant",
						"content": parts,
					})
				}

			case "tool":
				callID, _ := msg["tool_call_id"].(string)
				if callID == "" {
					callID, _ = msg["id"].(string)
				}
				outputStr := ""
				if c, ok := msg["content"].(string); ok {
					outputStr = c
				} else if b, err := json.Marshal(msg["content"]); err == nil {
					outputStr = string(b)
				}

				inputItems = append(inputItems, map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  outputStr,
				})
			}
		}
	} else {
		raw := stdReq.PromptTokenText
		if raw == "" {
			raw = stdReq.FinalPrompt
		}
		if raw == "" {
			raw = stdReq.HistoryText
		}
		if strings.TrimSpace(raw) != "" {
			inputItems = append(inputItems, map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": raw},
				},
			})
		}
	}

	return instructionsBuilder.String(), inputItems
}

func buildContentParts(rawContent any, defaultTextType string) []map[string]any {
	var parts []map[string]any

	switch v := rawContent.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			parts = append(parts, map[string]any{
				"type": defaultTextType,
				"text": v,
			})
		}
	case []any:
		for _, part := range v {
			pMap, ok := part.(map[string]any)
			if !ok {
				continue
			}
			pType, _ := pMap["type"].(string)
			switch pType {
			case "text":
				txt, _ := pMap["text"].(string)
				parts = append(parts, map[string]any{
					"type": defaultTextType,
					"text": txt,
				})
			case "image_url":
				if imgObj, ok := pMap["image_url"].(map[string]any); ok {
					urlStr, _ := imgObj["url"].(string)
					parts = append(parts, map[string]any{
						"type":      "input_image",
						"image_url": urlStr,
					})
				}
			}
		}
	}
	return parts
}

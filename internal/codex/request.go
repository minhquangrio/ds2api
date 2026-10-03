package codex

import (
	"strings"

	"ds2api/internal/promptcompat"
)

// DefaultFallbackModel is used only when neither the request nor the configured
// codex.default_model supplies a model, so the upstream never receives an empty
// model field.
const DefaultFallbackModel = "gpt-6-luna"

// BuildResponsesBody renders the upstream Responses payload using only the
// request's own model.
func BuildResponsesBody(stdReq promptcompat.StandardRequest) (map[string]any, error) {
	return BuildResponsesBodyWithDefault(stdReq, "")
}

// BuildResponsesBodyWithDefault renders the upstream Responses payload. When
// the request carries no usable model, fallbackModel is used so an empty model
// is never forwarded.
func BuildResponsesBodyWithDefault(stdReq promptcompat.StandardRequest, fallbackModel string) (map[string]any, error) {
	instructions, inputItems := BuildInput(stdReq)

	model := stdReq.ResolvedModel
	if model == "" {
		model = stdReq.RequestedModel
	}
	if model == "" {
		model = strings.TrimSpace(fallbackModel)
	}
	if model == "" {
		model = DefaultFallbackModel
	}

	body := map[string]any{
		"model":        model,
		"store":        false,
		"stream":       true,
		"instructions": instructions,
		"input":        inputItems,
		"text": map[string]any{
			"verbosity": "medium",
		},
		"include":             []string{"reasoning.encrypted_content"},
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
	}

	if stdReq.ToolChoice.IsNone() {
		body["tool_choice"] = "none"
	} else if stdReq.ToolChoice.ForcedName != "" {
		body["tool_choice"] = map[string]any{
			"type": "function",
			"name": stdReq.ToolChoice.ForcedName,
		}
	}

	// Tools
	if stdReq.ToolsRaw != nil {
		if rawSlice, ok := stdReq.ToolsRaw.([]any); ok && len(rawSlice) > 0 {
			var codexTools []map[string]any
			for _, item := range rawSlice {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}

				toolObj := map[string]any{
					"type": "function",
				}

				if fn, ok := itemMap["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						toolObj["name"] = name
					}
					if desc, ok := fn["description"].(string); ok {
						toolObj["description"] = desc
					}
					if params, ok := fn["parameters"]; ok {
						toolObj["parameters"] = params
					}
				} else {
					if name, ok := itemMap["name"].(string); ok {
						toolObj["name"] = name
					}
					if desc, ok := itemMap["description"].(string); ok {
						toolObj["description"] = desc
					}
					if params, ok := itemMap["parameters"]; ok {
						toolObj["parameters"] = params
					}
				}

				codexTools = append(codexTools, toolObj)
			}
			if len(codexTools) > 0 {
				body["tools"] = codexTools
			}
		}
	}

	return body, nil
}

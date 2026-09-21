package config

import (
	"strings"
	"time"
)

type ModelInfo struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Created    int64  `json:"created"`
	OwnedBy    string `json:"owned_by"`
	Permission []any  `json:"permission,omitempty"`
}
type OllamaModelInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}
type OllamaCapabilitiesModelInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
}

type ModelAliasReader interface {
	ModelAliases() map[string]string
}

type ModelTarget struct {
	Provider  string // "deepseek" | "gemini"
	Canonical string
	Variant   string // "nothinking", "search", etc.
}

const noThinkingModelSuffix = "-nothinking"

var deepSeekBaseModels = []ModelInfo{
	{ID: "deepseek-v4-flash", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-v4-pro", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-v4-flash-search", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
	{ID: "deepseek-v4-vision", Object: "model", Created: 1677610602, OwnedBy: "deepseek", Permission: []any{}},
}

// Gemini Web model names follow gemini-webapi's naming: a canonical name is
// derived from the model's category, so an account reports "gemini-pro",
// "gemini-flash" and "gemini-flash-lite". The Basic/Plus/Advanced tier of a given
// model belongs to the account and is discovered at session init - it is not a
// separate model - which is why the tier and versioned spellings live in the
// alias table below rather than in this catalogue.
var geminiBaseModels = []ModelInfo{
	{ID: "gemini-pro", Object: "model", Created: 1735689600, OwnedBy: "google"},
	{ID: "gemini-flash", Object: "model", Created: 1735689600, OwnedBy: "google"},
	{ID: "gemini-flash-lite", Object: "model", Created: 1735689600, OwnedBy: "google"},
}

var GeminiModels = appendNoThinkingVariants(geminiBaseModels)

var OllamaCapabilitiesModels = []OllamaCapabilitiesModelInfo{
	{ID: "deepseek-v4-flash", Capabilities: []string{"tools", "thinking"}},
	{ID: "deepseek-v4-pro", Capabilities: []string{"tools", "thinking"}},
	{ID: "deepseek-v4-flash-search", Capabilities: []string{"tools", "thinking"}},
	{ID: "deepseek-v4-vision", Capabilities: []string{"tools", "thinking", "vision"}},
	{ID: "deepseek-v4-flash-nothinking", Capabilities: []string{"tools"}},
	{ID: "deepseek-v4-pro-nothinking", Capabilities: []string{"tools"}},
	{ID: "deepseek-v4-flash-search-nothinking", Capabilities: []string{"tools"}},
	{ID: "deepseek-v4-vision-nothinking", Capabilities: []string{"tools", "vision"}},
}

var DeepSeekModels = appendNoThinkingVariants(deepSeekBaseModels)
var OllamaModels = mapToOllamaModels(DeepSeekModels)
var claudeBaseModels = []ModelInfo{
	// Current aliases
	{ID: "claude-opus-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-6", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 4.x snapshots and prior aliases kept for compatibility
	{ID: "claude-sonnet-4-5", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-1-20250805", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-opus-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-5-20250929", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-0", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-sonnet-4-20250514", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-haiku-4-5-20251001", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},

	// Claude 3.x (legacy/deprecated snapshots and aliases)
	{ID: "claude-3-7-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-7-sonnet-20250219", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20240620", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-sonnet-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-opus-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-sonnet-20240229", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-latest", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-5-haiku-20241022", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
	{ID: "claude-3-haiku-20240307", Object: "model", Created: 1715635200, OwnedBy: "anthropic"},
}

var ClaudeModels = appendNoThinkingVariants(claudeBaseModels)

func GetModelConfig(model string) (thinking bool, search bool, ok bool) {
	baseModel, noThinking := splitNoThinkingModel(model)
	if baseModel == "" {
		return false, false, false
	}
	switch baseModel {
	case "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-vision":
		return !noThinking, false, true
	case "deepseek-v4-flash-search":
		return !noThinking, true, true
	case "gemini-pro", "gemini-flash":
		return !noThinking, false, true
	case "gemini-flash-lite":
		return false, false, true
	default:
		return false, false, false
	}
}

func GetModelType(model string) (modelType string, ok bool) {
	baseModel, _ := splitNoThinkingModel(model)
	switch baseModel {
	case "deepseek-v4-flash", "deepseek-v4-flash-search":
		return "default", true
	case "deepseek-v4-pro":
		return "expert", true
	case "deepseek-v4-vision":
		return "vision", true
	case "gemini-pro":
		return "gemini_pro", true
	case "gemini-flash", "gemini-flash-lite":
		return "gemini_flash", true
	default:
		return "", false
	}
}

func IsSupportedDeepSeekModel(model string) bool {
	baseModel, _ := splitNoThinkingModel(model)
	switch baseModel {
	case "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-search", "deepseek-v4-vision":
		return true
	default:
		return false
	}
}

func IsSupportedGeminiModel(model string) bool {
	baseModel, _ := splitNoThinkingModel(model)
	switch baseModel {
	case "gemini-pro", "gemini-flash", "gemini-flash-lite":
		return true
	default:
		return false
	}
}

func IsNoThinkingModel(model string) bool {
	_, noThinking := splitNoThinkingModel(model)
	return noThinking
}

func DefaultModelAliases() map[string]string {
	return map[string]string{
		// OpenAI GPT / ChatGPT families
		"chatgpt-4o":          "deepseek-v4-flash",
		"gpt-4":               "deepseek-v4-flash",
		"gpt-4-turbo":         "deepseek-v4-flash",
		"gpt-4-turbo-preview": "deepseek-v4-flash",
		"gpt-4.5-preview":     "deepseek-v4-flash",
		"gpt-4o":              "deepseek-v4-flash",
		"gpt-4o-mini":         "deepseek-v4-flash",
		"gpt-4.1":             "deepseek-v4-flash",
		"gpt-4.1-mini":        "deepseek-v4-flash",
		"gpt-4.1-nano":        "deepseek-v4-flash",
		"gpt-5":               "deepseek-v4-flash",
		"gpt-5-chat":          "deepseek-v4-flash",
		"gpt-5.1":             "deepseek-v4-flash",
		"gpt-5.1-chat":        "deepseek-v4-flash",
		"gpt-5.2":             "deepseek-v4-flash",
		"gpt-5.2-chat":        "deepseek-v4-flash",
		"gpt-5.3-chat":        "deepseek-v4-flash",
		"gpt-5.4":             "deepseek-v4-flash",
		"gpt-5.5":             "deepseek-v4-flash",
		"gpt-5-mini":          "deepseek-v4-flash",
		"gpt-5-nano":          "deepseek-v4-flash",
		"gpt-5.4-mini":        "deepseek-v4-flash",
		"gpt-5.4-nano":        "deepseek-v4-flash",
		"gpt-5-pro":           "deepseek-v4-pro",
		"gpt-5.2-pro":         "deepseek-v4-pro",
		"gpt-5.4-pro":         "deepseek-v4-pro",
		"gpt-5.5-pro":         "deepseek-v4-pro",
		"gpt-5-codex":         "deepseek-v4-pro",
		"gpt-5.1-codex":       "deepseek-v4-pro",
		"gpt-5.1-codex-mini":  "deepseek-v4-pro",
		"gpt-5.1-codex-max":   "deepseek-v4-pro",
		"gpt-5.2-codex":       "deepseek-v4-pro",
		"gpt-5.3-codex":       "deepseek-v4-pro",
		"codex-mini-latest":   "deepseek-v4-pro",

		// OpenAI reasoning / research families
		"o1":                    "deepseek-v4-pro",
		"o1-preview":            "deepseek-v4-pro",
		"o1-mini":               "deepseek-v4-pro",
		"o1-pro":                "deepseek-v4-pro",
		"o3":                    "deepseek-v4-pro",
		"o3-mini":               "deepseek-v4-pro",
		"o3-pro":                "deepseek-v4-pro",
		"o3-deep-research":      "deepseek-v4-flash-search",
		"o4-mini":               "deepseek-v4-pro",
		"o4-mini-deep-research": "deepseek-v4-flash-search",

		// Claude current and historical aliases
		"claude-opus-4-6":            "deepseek-v4-pro",
		"claude-opus-4-1":            "deepseek-v4-pro",
		"claude-opus-4-1-20250805":   "deepseek-v4-pro",
		"claude-opus-4-0":            "deepseek-v4-pro",
		"claude-opus-4-20250514":     "deepseek-v4-pro",
		"claude-sonnet-4-6":          "deepseek-v4-flash",
		"claude-sonnet-4-5":          "deepseek-v4-flash",
		"claude-sonnet-4-5-20250929": "deepseek-v4-flash",
		"claude-sonnet-4-0":          "deepseek-v4-flash",
		"claude-sonnet-4-20250514":   "deepseek-v4-flash",
		"claude-haiku-4-5":           "deepseek-v4-flash",
		"claude-haiku-4-5-20251001":  "deepseek-v4-flash",
		"claude-3-7-sonnet":          "deepseek-v4-flash",
		"claude-3-7-sonnet-latest":   "deepseek-v4-flash",
		"claude-3-7-sonnet-20250219": "deepseek-v4-flash",
		"claude-3-5-sonnet":          "deepseek-v4-flash",
		"claude-3-5-sonnet-latest":   "deepseek-v4-flash",
		"claude-3-5-sonnet-20240620": "deepseek-v4-flash",
		"claude-3-5-sonnet-20241022": "deepseek-v4-flash",
		"claude-3-5-haiku":           "deepseek-v4-flash",
		"claude-3-5-haiku-latest":    "deepseek-v4-flash",
		"claude-3-5-haiku-20241022":  "deepseek-v4-flash",
		"claude-3-opus":              "deepseek-v4-pro",
		"claude-3-opus-20240229":     "deepseek-v4-pro",
		"claude-3-sonnet":            "deepseek-v4-flash",
		"claude-3-sonnet-20240229":   "deepseek-v4-flash",
		"claude-3-haiku":             "deepseek-v4-flash",
		"claude-3-haiku-20240307":    "deepseek-v4-flash",

		// Gemini: every accepted spelling of the three models above. gemini-webapi
		// derives a model's canonical name from its category and treats the
		// Plus/Advanced variants as the same model, because the tier belongs to the
		// account and is read from it at session init - so all of these resolve to
		// one entry, and Canonical is always a name GeminiModels advertises.
		"gemini-pro-latest":   "gemini-pro",
		"gemini-pro-vision":   "gemini-pro",
		"gemini-pro-plus":     "gemini-pro",
		"gemini-pro-advanced": "gemini-pro",
		"gemini-1.5-pro":      "gemini-pro",
		"gemini-2.5-pro":      "gemini-pro",
		"gemini-3-pro":        "gemini-pro",
		"gemini-3.1-pro":      "gemini-pro",

		"gemini-flash-latest":   "gemini-flash",
		"gemini-flash-plus":     "gemini-flash",
		"gemini-flash-advanced": "gemini-flash",
		"gemini-1.5-flash":      "gemini-flash",
		"gemini-1.5-flash-8b":   "gemini-flash",
		"gemini-2.0-flash":      "gemini-flash",
		"gemini-2.5-flash":      "gemini-flash",
		"gemini-3-flash":        "gemini-flash",
		"gemini-3.1-flash":      "gemini-flash",

		"gemini-flash-lite-plus":     "gemini-flash-lite",
		"gemini-flash-lite-advanced": "gemini-flash-lite",
		"gemini-2.0-flash-lite":      "gemini-flash-lite",
		"gemini-2.5-flash-lite":      "gemini-flash-lite",
		"gemini-3-flash-lite":        "gemini-flash-lite",
		"gemini-3.1-flash-lite":      "gemini-flash-lite",

		"llama-3.1-70b-instruct": "deepseek-v4-flash",
		"qwen-max":               "deepseek-v4-flash",
	}
}

func ResolveModelTarget(store ModelAliasReader, requested string) (ModelTarget, bool) {
	model := lower(strings.TrimSpace(requested))
	if model == "" {
		return ModelTarget{}, false
	}
	baseModel, noThinking := splitNoThinkingModel(model)

	// 1. Direct DeepSeek models
	if IsSupportedDeepSeekModel(baseModel) {
		variant := ""
		if noThinking {
			variant = "nothinking"
		} else if strings.Contains(baseModel, "search") {
			variant = "search"
		}
		return ModelTarget{
			Provider:  "deepseek",
			Canonical: model,
			Variant:   variant,
		}, true
	}

	// 2. Direct Gemini models
	if IsSupportedGeminiModel(baseModel) {
		variant := ""
		if noThinking {
			variant = "nothinking"
		}
		return ModelTarget{
			Provider:  "gemini",
			Canonical: model,
			Variant:   variant,
		}, true
	}

	// 3. Alias lookup
	aliases := loadModelAliases(store)
	if mapped, ok := aliases[model]; ok {
		mappedBase, mappedNoThinking := splitNoThinkingModel(mapped)
		if IsSupportedDeepSeekModel(mappedBase) {
			variant := ""
			if mappedNoThinking {
				variant = "nothinking"
			} else if strings.Contains(mappedBase, "search") {
				variant = "search"
			}
			return ModelTarget{
				Provider:  "deepseek",
				Canonical: mapped,
				Variant:   variant,
			}, true
		}
		if IsSupportedGeminiModel(mappedBase) {
			variant := ""
			if mappedNoThinking {
				variant = "nothinking"
			}
			return ModelTarget{
				Provider:  "gemini",
				Canonical: mapped,
				Variant:   variant,
			}, true
		}
	}

	// 4. Base model alias lookup with preserved suffix
	if mapped, ok := aliases[baseModel]; ok {
		mappedBase, mappedNoThinking := splitNoThinkingModel(mapped)
		effectiveNoThinking := noThinking || mappedNoThinking
		canonical := withNoThinkingVariant(mappedBase, effectiveNoThinking)
		if IsSupportedDeepSeekModel(mappedBase) {
			variant := ""
			if effectiveNoThinking {
				variant = "nothinking"
			} else if strings.Contains(mappedBase, "search") {
				variant = "search"
			}
			return ModelTarget{
				Provider:  "deepseek",
				Canonical: canonical,
				Variant:   variant,
			}, true
		}
		if IsSupportedGeminiModel(mappedBase) {
			variant := ""
			if effectiveNoThinking {
				variant = "nothinking"
			}
			return ModelTarget{
				Provider:  "gemini",
				Canonical: canonical,
				Variant:   variant,
			}, true
		}
	}

	return ModelTarget{}, false
}

func ResolveModel(store ModelAliasReader, requested string) (string, bool) {
	target, ok := ResolveModelTarget(store, requested)
	if !ok {
		return "", false
	}
	return target.Canonical, true
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func OpenAIModelsResponse() map[string]any {
	all := make([]ModelInfo, 0, len(DeepSeekModels)+len(GeminiModels))
	all = append(all, DeepSeekModels...)
	all = append(all, GeminiModels...)
	return map[string]any{"object": "list", "data": all}
}

func OpenAIModelByID(store ModelAliasReader, id string) (ModelInfo, bool) {
	target, ok := ResolveModelTarget(store, id)
	if !ok {
		return ModelInfo{}, false
	}
	if target.Provider == "gemini" {
		for _, model := range GeminiModels {
			if model.ID == target.Canonical {
				return model, true
			}
		}
	} else {
		for _, model := range DeepSeekModels {
			if model.ID == target.Canonical {
				return model, true
			}
		}
	}
	return ModelInfo{}, false
}

func OllamaModelsResponse() map[string]any {
	return map[string]any{"models": OllamaModels}
}

func OllamaModelByID(store ModelAliasReader, id string) (OllamaCapabilitiesModelInfo, bool) {
	target, ok := ResolveModelTarget(store, id)
	if !ok {
		return OllamaCapabilitiesModelInfo{}, false
	}
	for _, model := range OllamaCapabilitiesModels {
		if model.ID == target.Canonical {
			return model, true
		}
	}
	return OllamaCapabilitiesModelInfo{}, false
}

func ClaudeModelsResponse() map[string]any {
	resp := map[string]any{"object": "list", "data": ClaudeModels}
	if len(ClaudeModels) > 0 {
		resp["first_id"] = ClaudeModels[0].ID
		resp["last_id"] = ClaudeModels[len(ClaudeModels)-1].ID
	} else {
		resp["first_id"] = nil
		resp["last_id"] = nil
	}
	resp["has_more"] = false
	return resp
}

func appendNoThinkingVariants(models []ModelInfo) []ModelInfo {
	out := make([]ModelInfo, 0, len(models)*2)
	for _, model := range models {
		out = append(out, model)
		variant := model
		variant.ID = withNoThinkingVariant(model.ID, true)
		out = append(out, variant)
	}
	return out
}
func mapToOllamaModels(models []ModelInfo) []OllamaModelInfo {
	out := make([]OllamaModelInfo, 0, len(models))
	for _, model := range models {
		var modifiedAt string
		if model.Created > 0 {
			modifiedAt = time.Unix(model.Created, 0).Format(time.RFC3339)
		}
		ollamaModel := OllamaModelInfo{
			Name:       model.ID,
			Model:      model.ID,
			Size:       0,
			ModifiedAt: modifiedAt,
		}
		out = append(out, ollamaModel)
	}
	return out
}

func splitNoThinkingModel(model string) (string, bool) {
	model = lower(strings.TrimSpace(model))
	if strings.HasSuffix(model, noThinkingModelSuffix) {
		return strings.TrimSuffix(model, noThinkingModelSuffix), true
	}
	return model, false
}

func withNoThinkingVariant(model string, enabled bool) string {
	baseModel, _ := splitNoThinkingModel(model)
	if !enabled {
		return baseModel
	}
	if baseModel == "" {
		return ""
	}
	return baseModel + noThinkingModelSuffix
}

func loadModelAliases(store ModelAliasReader) map[string]string {
	aliases := DefaultModelAliases()
	if store != nil {
		for k, v := range store.ModelAliases() {
			aliases[lower(strings.TrimSpace(k))] = lower(strings.TrimSpace(v))
		}
	}
	return aliases
}

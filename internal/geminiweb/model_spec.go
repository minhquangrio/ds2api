package geminiweb

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// capacityFieldVersioned marks accounts whose capacity slot carries an extra
// leading element instead of a bare number: the tail becomes "null,<capacity>"
// rather than "<capacity>". Mirrors compute_capacity() in gemini-webapi
// (types/availablemodel.py).
const (
	capacityFieldVersioned = 13
	capacityFieldDefault   = 12
)

// ModelSpec is one selectable Gemini Web model.
//
// ModelID, Capacity and CapacityField are discovered per account at session
// init (see DiscoverModels) because Google renumbers and retiers models over
// time. gemini-webapi warns about this explicitly: a hardcoded id "goes stale
// whenever Google renames, renumbers or retiers a model", and the Plus/Advanced
// variants "ask the caller to know their own account tier - the same value
// compute_capacity reads from the account itself".
//
// defaultModelSpecs below is therefore only a last-resort fallback, pinned to
// the free tier, for when discovery has not run or matched nothing.
type ModelSpec struct {
	ModelID       string
	Capacity      int
	CapacityField int
	ModelNumber   int
	AdvancedOnly  bool
}

// defaultModelSpecs is the free-tier fallback set.
//
// The model ids differ per tier: a free account must ask for the basic ids with
// capacity 1, while Plus/Advanced accounts use different ids with capacity 4/2.
// Discovery resolves this properly; these values only keep a free account
// working if that RPC fails.
var defaultModelSpecs = map[string]ModelSpec{
	"gemini-pro": {
		ModelID: "9d8ca3786ebdfbea", Capacity: 1, CapacityField: capacityFieldDefault, ModelNumber: 3,
	},
	"gemini-flash": {
		ModelID: "fbb127bbb056c959", Capacity: 1, CapacityField: capacityFieldDefault, ModelNumber: 1,
	},
	"gemini-flash-lite": {
		ModelID: "cf41b0e0dd7d53e5", Capacity: 1, CapacityField: capacityFieldDefault, ModelNumber: 6,
	},
}

// ResolveModelSpec maps a requested model name onto a ModelSpec, preferring the
// models discovered for this account over the static fallback.
func (c *Client) ResolveModelSpec(modelName string) ModelSpec {
	name := strings.ToLower(strings.TrimSpace(modelName))
	name = strings.TrimSuffix(name, "-nothinking")

	discovered := c.discoveredSpecs()
	if spec, ok := discovered[name]; ok {
		return spec
	}
	if spec, ok := defaultModelSpecs[name]; ok {
		return spec
	}

	// A name Gemini Web never reports still belongs to a family. The library
	// parses only the major version out of a display name, so a client-facing
	// alias like "gemini-3.1-flash" is never an alias of the discovered model -
	// it would otherwise fall straight through to the static table and send the
	// free-tier id and capacity on a paid account. Resolve it against the
	// account's discovered model for the family instead.
	family := familyKey(name)
	if spec, ok := discovered[family]; ok {
		return spec
	}
	return defaultModelSpecs[family]
}

// familyKey reduces a model name to the fallback key for its family.
func familyKey(name string) string {
	switch {
	case strings.Contains(name, "pro"):
		return "gemini-pro"
	case strings.Contains(name, "lite"):
		return "gemini-flash-lite"
	default:
		return "gemini-flash"
	}
}

// BuildModelHeader renders the x-goog-ext-525001261-jspb model-selection header.
//
// The capacity slot is emitted raw because the versioned form is a two-element
// fragment ("null,2"), not a single JSON value - hence json.RawMessage rather
// than an int. The trailing thinking flag and per-client session id are
// appended last, matching _generate() in gemini-webapi's client.py.
func BuildModelHeader(spec ModelSpec, extendedThinking bool, clientSessionID string) string {
	thinkingVal := 1
	if extendedThinking {
		thinkingVal = 2
	}

	headerElements := []any{
		1,
		nil,
		nil,
		nil,
		spec.ModelID,
		nil,
		nil,
		0,
		[]any{4, 5, 6, 8},
		nil,
		nil,
	}
	if spec.CapacityField == capacityFieldVersioned {
		headerElements = append(headerElements, nil, spec.Capacity)
	} else {
		headerElements = append(headerElements, spec.Capacity)
	}
	headerElements = append(headerElements,
		nil,
		nil,
		spec.ModelNumber,
		thinkingVal,
		clientSessionID,
	)

	b, err := json.Marshal(headerElements)
	if err != nil {
		return ""
	}
	return string(b)
}

// computeCapacity derives (capacity, capacityField) from an account's tier and
// capability flags. Direct port of AvailableModel.compute_capacity().
func computeCapacity(tierFlags, capabilityFlags []any) (int, int) {
	if containsNumber(tierFlags, 21) {
		return 1, capacityFieldVersioned
	}
	if containsNumber(tierFlags, 22) {
		return 2, capacityFieldVersioned
	}
	if containsNumber(capabilityFlags, 115) {
		return 4, capacityFieldDefault // Plus accounts
	}
	if containsNumber(tierFlags, 16) || containsNumber(capabilityFlags, 106) {
		return 3, capacityFieldDefault // Pro accounts (uncommon)
	}
	if containsNumber(tierFlags, 8) || containsNumber(capabilityFlags, 19) {
		return 2, capacityFieldDefault // Pro accounts
	}
	return 1, capacityFieldDefault // Free accounts
}

var modelVersionRe = regexp.MustCompile(`(\d+)(?:\.\d+)?`)

// deriveNameAndAliases derives a canonical model name plus lookup aliases from a
// discovered model's category and display names. Direct port of
// AvailableModel._derive_name_and_aliases(), which is deliberately generic so
// that renamed or renumbered models still resolve.
func deriveNameAndAliases(modelID, categoryName, displayName string) (string, []string) {
	aliases := map[string]struct{}{}
	if modelID != "" {
		aliases[strings.ToLower(modelID)] = struct{}{}
	}

	catClean := strings.TrimSpace(categoryName)
	dispClean := strings.TrimSpace(displayName)

	majorVer := ""
	if m := modelVersionRe.FindStringSubmatch(dispClean); len(m) > 1 {
		majorVer = m[1]
	}

	catSlug := ""
	if catClean != "" {
		catSlug = strings.ReplaceAll(strings.ToLower(catClean), " ", "-")
		aliases[strings.ToLower(catClean)] = struct{}{}
		aliases[catSlug] = struct{}{}
		aliases["gemini-"+catSlug] = struct{}{}
		if majorVer != "" {
			aliases["gemini-"+majorVer+"-"+catSlug] = struct{}{}
		}
	}

	dispSlug := ""
	if dispClean != "" {
		dispSlug = strings.ReplaceAll(strings.ToLower(dispClean), " ", "-")
		aliases[strings.ToLower(dispClean)] = struct{}{}
		aliases[dispSlug] = struct{}{}
		aliases["gemini-"+dispSlug] = struct{}{}
	}

	var primary string
	switch {
	case catSlug != "":
		primary = "gemini-" + catSlug
	case dispSlug != "":
		primary = "gemini-" + dispSlug
	default:
		primary = "gemini-" + modelID
	}
	aliases[primary] = struct{}{}

	out := make([]string, 0, len(aliases))
	for a := range aliases {
		out = append(out, a)
	}
	sort.Strings(out)
	return primary, out
}

// containsNumber reports whether a decoded JSON list holds the given number.
// Decoded JSON numbers are float64, so equality is compared numerically.
func containsNumber(list []any, want int) bool {
	for _, v := range list {
		if n, ok := toInt(v); ok && n == want {
			return true
		}
	}
	return false
}

package geminiweb

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveModelSpecFallsBackPerFamily(t *testing.T) {
	c := &Client{modelSpecs: map[string]ModelSpec{}}

	pro := c.ResolveModelSpec("gemini-3-pro")
	if pro.ModelNumber != 3 {
		t.Errorf("expected model number 3 for Pro, got %d", pro.ModelNumber)
	}
	flash := c.ResolveModelSpec("gemini-3-flash")
	if flash.ModelNumber != 1 {
		t.Errorf("expected model number 1 for Flash, got %d", flash.ModelNumber)
	}
	lite := c.ResolveModelSpec("gemini-3.1-flash-lite")
	if lite.ModelNumber != 6 {
		t.Errorf("expected model number 6 for Lite, got %d", lite.ModelNumber)
	}

	// The fallback set is the free tier: capacity 1, capacity field 12.
	if flash.Capacity != 1 || flash.CapacityField != capacityFieldDefault {
		t.Errorf("fallback should be free tier, got capacity=%d field=%d", flash.Capacity, flash.CapacityField)
	}

	// Unknown names still land on a family rather than an empty spec.
	if got := c.ResolveModelSpec("gemini-2.5-pro-preview"); got.ModelNumber != 3 {
		t.Errorf("unknown pro name should map to the pro fallback, got %+v", got)
	}
}

// TestResolveModelSpecPrefersDiscovery is the point of the discovery step: the
// model id and tier capacity belong to the account, not to a static table.
func TestResolveModelSpecPrefersDiscovery(t *testing.T) {
	discovered := ModelSpec{
		ModelID: "discovered-flash-id", Capacity: 4, CapacityField: capacityFieldDefault, ModelNumber: 1,
	}
	c := &Client{modelSpecs: map[string]ModelSpec{"gemini-3-flash": discovered}}

	got := c.ResolveModelSpec("gemini-3-flash")
	if got.ModelID != "discovered-flash-id" || got.Capacity != 4 {
		t.Errorf("discovery must win over the fallback table, got %+v", got)
	}
}

// TestResolveModelSpecAliasKeepsAccountTier covers a client-facing alias Gemini
// Web never reports. The library's version parsing reads only the major version,
// so "gemini-3.1-flash" is not an alias of the discovered model; without the
// family fallback it would bypass discovery and send the free-tier id and
// capacity on a paid account.
func TestResolveModelSpecAliasKeepsAccountTier(t *testing.T) {
	discovered := ModelSpec{
		ModelID: "paid-flash-id", Capacity: 4, CapacityField: capacityFieldDefault, ModelNumber: 1,
	}
	c := &Client{modelSpecs: map[string]ModelSpec{"gemini-flash": discovered}}

	for _, alias := range []string{"gemini-3.1-flash", "gemini-2.5-flash", "gemini-flash-latest"} {
		got := c.ResolveModelSpec(alias)
		if got.ModelID != "paid-flash-id" || got.Capacity != 4 {
			t.Errorf("%s should resolve to the account's discovered flash model, got %+v", alias, got)
		}
	}

	// With nothing discovered the static free-tier fallback still applies.
	empty := &Client{modelSpecs: map[string]ModelSpec{}}
	if got := empty.ResolveModelSpec("gemini-3.1-flash"); got.ModelID != defaultModelSpecs["gemini-flash"].ModelID {
		t.Errorf("expected static fallback with no discovery, got %+v", got)
	}
}

func TestComputeCapacity(t *testing.T) {
	cases := []struct {
		name             string
		tier, capability []any
		wantCapacity     int
		wantField        int
	}{
		{"free account", []any{}, []any{}, 1, capacityFieldDefault},
		{"pro via tier 8", []any{float64(8)}, []any{}, 2, capacityFieldDefault},
		{"pro via capability 19", []any{}, []any{float64(19)}, 2, capacityFieldDefault},
		{"pro uncommon", []any{float64(16)}, []any{}, 3, capacityFieldDefault},
		{"plus", []any{}, []any{float64(115)}, 4, capacityFieldDefault},
		{"versioned tier 21", []any{float64(21)}, []any{}, 1, capacityFieldVersioned},
		{"versioned tier 22", []any{float64(22)}, []any{}, 2, capacityFieldVersioned},
		{"versioned wins over plus", []any{float64(21)}, []any{float64(115)}, 1, capacityFieldVersioned},
	}
	for _, tc := range cases {
		capacity, field := computeCapacity(tc.tier, tc.capability)
		if capacity != tc.wantCapacity || field != tc.wantField {
			t.Errorf("%s: got (%d,%d), want (%d,%d)", tc.name, capacity, field, tc.wantCapacity, tc.wantField)
		}
	}
}

func TestBuildModelHeader(t *testing.T) {
	spec := ModelSpec{
		ModelID:       "test_model_id",
		Capacity:      2,
		CapacityField: capacityFieldDefault,
		ModelNumber:   3,
	}
	header := BuildModelHeader(spec, true, "session-123")

	if !strings.Contains(header, `"test_model_id"`) {
		t.Errorf("header missing model_id: %s", header)
	}
	if !strings.Contains(header, `"session-123"`) {
		t.Errorf("header missing session_id: %s", header)
	}
	if !strings.Contains(header, ",2,") {
		t.Errorf("header missing extended thinking indicator: %s", header)
	}

	var parsed []any
	if err := json.Unmarshal([]byte(header), &parsed); err != nil {
		t.Fatalf("header is not valid JSON: %v (%s)", err, header)
	}
	if len(parsed) != 17 {
		t.Errorf("expected 17 header elements, got %d", len(parsed))
	}
	// The trailing pair is the thinking flag then the client session id.
	if v, ok := parsed[15].(float64); !ok || int(v) != 2 {
		t.Errorf("element 15 should be the thinking flag 2, got %v", parsed[15])
	}
	if s, ok := parsed[16].(string); !ok || s != "session-123" {
		t.Errorf("element 16 should be the session id, got %v", parsed[16])
	}
}

// TestBuildModelHeaderVersionedCapacity covers capacity_field == 13, where the
// capacity slot expands into two JSON elements rather than one number.
func TestBuildModelHeaderVersionedCapacity(t *testing.T) {
	spec := ModelSpec{
		ModelID:       "versioned_id",
		Capacity:      2,
		CapacityField: capacityFieldVersioned,
		ModelNumber:   3,
	}
	header := BuildModelHeader(spec, false, "sid")

	var parsed []any
	if err := json.Unmarshal([]byte(header), &parsed); err != nil {
		t.Fatalf("versioned capacity header is not valid JSON: %v (%s)", err, header)
	}
	if len(parsed) != 18 {
		t.Fatalf("versioned capacity should widen the header to 18 elements, got %d", len(parsed))
	}
	if parsed[11] != nil {
		t.Errorf("expected a null element in the widened capacity slot, got %v", parsed[11])
	}
	if v, ok := parsed[12].(float64); !ok || int(v) != 2 {
		t.Errorf("expected capacity 2 after the null, got %v", parsed[12])
	}
}

func TestDeriveNameAndAliases(t *testing.T) {
	primary, aliases := deriveNameAndAliases("abc123", "Flash", "Gemini 3 Flash")

	if primary != "gemini-flash" {
		t.Errorf("unexpected primary name %q", primary)
	}
	for _, want := range []string{"gemini-flash", "gemini-3-flash", "flash", "abc123"} {
		found := false
		for _, a := range aliases {
			if a == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("alias %q missing from %v", want, aliases)
		}
	}
}

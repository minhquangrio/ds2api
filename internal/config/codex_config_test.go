package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexAccountConfig(t *testing.T) {
	acc := Account{
		Provider:           "codex",
		CodexRefreshToken:  "refresh_123",
		CodexAccountID:     "acc_456",
		CodexExpiresAt:     1735689600,
		CodexPlanType:      "plus",
		CodexAccountSource: "chatgpt",
	}

	if !acc.IsCodex() {
		t.Errorf("expected acc.IsCodex() == true")
	}
	if !acc.IsSchedulable() {
		t.Errorf("expected acc.IsSchedulable() == true")
	}
	if acc.Identifier() != "codex:acc_456" {
		t.Errorf("expected identifier 'codex:acc_456', got %q", acc.Identifier())
	}

	// Missing refresh token makes it unschedulable
	accNoRefresh := acc
	accNoRefresh.CodexRefreshToken = ""
	if accNoRefresh.IsSchedulable() {
		t.Errorf("expected acc without refresh token to not be schedulable")
	}

	// ResolveModelTarget for direct model
	store := LoadStore()
	target, ok := ResolveModelTarget(store, "gpt-6-luna")
	if !ok || target.Provider != "codex" || target.Canonical != "gpt-6-luna" {
		t.Errorf("ResolveModelTarget(gpt-6-luna) = %+v, %v", target, ok)
	}

	// ResolveModelTarget with codex/ prefix
	targetPrefix, ok := ResolveModelTarget(store, "codex/my-custom-model")
	if !ok || targetPrefix.Provider != "codex" || targetPrefix.Canonical != "my-custom-model" {
		t.Errorf("ResolveModelTarget(codex/my-custom-model) = %+v, %v", targetPrefix, ok)
	}
}

func TestCodexEnabledDefaultsToTrue(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[]}`)
	if !LoadStore().CodexEnabled() {
		t.Errorf("an unset codex.enabled must keep the provider usable")
	}

	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[],"codex":{"enabled":false}}`)
	if LoadStore().CodexEnabled() {
		t.Errorf("codex.enabled=false must disable the provider")
	}

	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[],"codex":{"enabled":true,"default_model":"gpt-6-sol"}}`)
	store := LoadStore()
	if !store.CodexEnabled() {
		t.Errorf("codex.enabled=true must keep the provider usable")
	}
	if got := store.CodexDefaultModel(); got != "gpt-6-sol" {
		t.Errorf("CodexDefaultModel() = %q; want gpt-6-sol", got)
	}
}

// TestCodexConfigSurvivesRoundTrip guards the silent-drop bug: an unknown key
// in the fixed-field decoder lands in AdditionalFields and is then lost on the
// next save.
func TestCodexConfigSurvivesRoundTrip(t *testing.T) {
	enabled := false
	original := Config{Codex: CodexConfig{
		Enabled:            &enabled,
		DefaultModel:       "gpt-6-sol",
		RefreshSkewSeconds: 120,
	}}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"codex"`) {
		t.Fatalf("codex block missing from encoded config: %s", encoded)
	}

	var decoded Config
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Codex.Enabled == nil || *decoded.Codex.Enabled {
		t.Errorf("codex.enabled did not round-trip: %+v", decoded.Codex)
	}
	if decoded.Codex.DefaultModel != "gpt-6-sol" {
		t.Errorf("default_model = %q; want gpt-6-sol", decoded.Codex.DefaultModel)
	}
	if decoded.Codex.RefreshSkewSeconds != 120 {
		t.Errorf("refresh_skew_seconds = %d; want 120", decoded.Codex.RefreshSkewSeconds)
	}
	if _, leaked := decoded.AdditionalFields["codex"]; leaked {
		t.Errorf("codex must be a decoded field, not an additional field")
	}
}

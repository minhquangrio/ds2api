package config

import (
	"testing"
)

func TestMuseAccountConfig(t *testing.T) {
	acc := Account{
		Name:     "muse-test",
		Provider: "muse",
		Cookies:  "hatch_sess=test_session_123",
	}

	if !acc.IsMuse() {
		t.Errorf("expected acc.IsMuse() == true")
	}
	if !acc.IsSchedulable() {
		t.Errorf("expected acc.IsSchedulable() == true")
	}
	if acc.Identifier() != "muse-test" {
		t.Errorf("expected identifier 'muse-test', got %q", acc.Identifier())
	}

	// Missing cookies makes it unschedulable
	accNoCookies := acc
	accNoCookies.Cookies = ""
	if accNoCookies.IsSchedulable() {
		t.Errorf("expected acc without cookies to not be schedulable")
	}

	// ResolveModelTarget for direct model
	store := LoadStore()
	target, ok := ResolveModelTarget(store, "muse-spark")
	if !ok || target.Provider != "muse" || target.Canonical != "muse-spark" {
		t.Errorf("ResolveModelTarget(muse-spark) = %+v, %v", target, ok)
	}

	// ResolveModelTarget with muse/ prefix
	targetPrefix, ok := ResolveModelTarget(store, "muse/my-custom-model")
	if !ok || targetPrefix.Provider != "muse" || targetPrefix.Canonical != "my-custom-model" {
		t.Errorf("ResolveModelTarget(muse/my-custom-model) = %+v, %v", targetPrefix, ok)
	}

	// OpenAIModelByID
	info, found := OpenAIModelByID(store, "muse-spark")
	if !found || info.ID != "muse-spark" {
		t.Errorf("OpenAIModelByID(muse-spark) = %+v, %v", info, found)
	}
}

func TestMuseEnabledDefaultsToTrue(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[]}`)
	if !LoadStore().MuseEnabled() {
		t.Errorf("an unset muse.enabled must keep the provider usable")
	}

	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[],"muse":{"enabled":false}}`)
	if LoadStore().MuseEnabled() {
		t.Errorf("muse.enabled=false must disable the provider")
	}
}

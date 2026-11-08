package config

import "testing"

func modelIDs(t *testing.T, response map[string]any) []ModelInfo {
	t.Helper()
	data, ok := response["data"].([]ModelInfo)
	if !ok {
		t.Fatalf("data is not []ModelInfo: %T", response["data"])
	}
	return data
}

func countOwner(models []ModelInfo, owner string) int {
	total := 0
	for _, model := range models {
		if model.OwnedBy == owner {
			total++
		}
	}
	return total
}

func TestOpenAIModelsResponseExcludingOwners(t *testing.T) {
	full := modelIDs(t, OpenAIModelsResponse())
	if countOwner(full, "muse") == 0 {
		t.Fatal("expected muse models in the default catalog")
	}

	filtered := modelIDs(t, OpenAIModelsResponseExcludingOwners("muse"))
	if countOwner(filtered, "muse") != 0 {
		t.Fatalf("muse models survived the filter: %+v", filtered)
	}
	if countOwner(filtered, "deepseek") != countOwner(full, "deepseek") {
		t.Fatal("filtering muse must not drop other providers")
	}

	if len(modelIDs(t, OpenAIModelsResponseExcludingOwners())) != len(full) {
		t.Fatal("no owners given should return the full catalog")
	}
	if countOwner(modelIDs(t, OpenAIModelsResponseExcludingOwners("  MUSE  ", "unknown")), "muse") != 0 {
		t.Fatal("owner matching must be case and whitespace insensitive")
	}
}

func TestMuseCatalogContainsMediaModels(t *testing.T) {
	ids := map[string]bool{}
	for _, model := range modelIDs(t, OpenAIModelsResponse()) {
		ids[model.ID] = true
	}
	for _, want := range []string{"muse-spark", "muse-chat", "muse-image", "muse-video"} {
		if !ids[want] {
			t.Fatalf("catalog is missing %q", want)
		}
	}
}

package shared

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"ds2api/internal/config"
)

func loadStoreWithMuse(t *testing.T, enabled bool) *config.Store {
	t.Helper()
	t.Setenv("DS2API_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	state := "true"
	if !enabled {
		state = "false"
	}
	t.Setenv("DS2API_CONFIG_JSON", `{"muse":{"enabled": `+state+
		`},"keys":["k"],"accounts":[{"name":"m1","provider":"muse","cookies":"hatch_sess=x"}]}`)
	store := config.LoadStore()
	return store
}

func listedOwners(t *testing.T, store *config.Store) map[string]int {
	t.Helper()
	handler := &ModelsHandler{Store: store}
	rec := httptest.NewRecorder()
	handler.ListModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	counts := map[string]int{}
	for _, model := range payload.Data {
		counts[model.OwnedBy]++
	}
	return counts
}

func TestListModelsHidesMuseWhenProviderDisabled(t *testing.T) {
	counts := listedOwners(t, loadStoreWithMuse(t, false))
	if counts["muse"] != 0 {
		t.Fatalf("muse models must not be advertised when disabled: %+v", counts)
	}
	if counts["deepseek"] == 0 {
		t.Fatalf("deepseek catalog entry vanished: %+v", counts)
	}
}

func TestListModelsAdvertisesMuseWhenProviderEnabled(t *testing.T) {
	counts := listedOwners(t, loadStoreWithMuse(t, true))
	if counts["muse"] == 0 {
		t.Fatalf("muse models should be listed when enabled: %+v", counts)
	}
}

func TestListModelsKeepsAllModelsForStoresWithoutAvailability(t *testing.T) {
	handler := &ModelsHandler{Store: nil}
	rec := httptest.NewRecorder()
	handler.ListModels(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := payload["data"].([]any)
	if len(data) == 0 {
		t.Fatal("a nil store must still return the full catalog, not an empty list")
	}
}

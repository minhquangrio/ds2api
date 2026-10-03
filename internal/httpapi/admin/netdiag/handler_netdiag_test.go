package netdiag

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	netdiagpkg "ds2api/internal/netdiag"
)

func TestDetectNetworkEndpoint(t *testing.T) {
	t.Setenv("DS2API_CONFIG_JSON", `{"accounts":[]}`)
	store := config.LoadStore()
	h := &Handler{Store: store}

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	req := httptest.NewRequest(http.MethodGet, "/network-detect", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var rep netdiagpkg.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if rep.Timestamp == 0 {
		t.Errorf("rep.Timestamp should not be 0")
	}
}

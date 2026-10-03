package imagebed

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	imagebedpkg "ds2api/internal/imagebed"
)

func TestImageBedAdminRoutes(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DS2API_IMAGE_BED_PATH", filepath.Join(tmpDir, "image_bed.json"))
	t.Setenv("DS2API_IMAGE_BED_HISTORY_PATH", filepath.Join(tmpDir, "history.json"))

	store := config.LoadStore()
	h := &Handler{Store: store}

	r := chi.NewRouter()
	RegisterRoutes(r, h)

	// 1. Save config
	body := `{"token":"ghp_test","owner":"myuser","repository":"myrepo","path_prefix":"imgs","branch":"main"}`
	req := httptest.NewRequest(http.MethodPut, "/image-bed/config", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// 2. Get config
	req = httptest.NewRequest(http.MethodGet, "/image-bed/config", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var cfgResp imagebedpkg.ConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &cfgResp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !cfgResp.HasToken || cfgResp.Owner != "myuser" {
		t.Fatalf("unexpected config response: %+v", cfgResp)
	}

	// 3. History is empty initially
	req = httptest.NewRequest(http.MethodGet, "/image-bed/history", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 4. Delete config
	req = httptest.NewRequest(http.MethodDelete, "/image-bed/config", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

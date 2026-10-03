package imagebed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"test.png", "test.png"},
		{"../../evil.jpg", "evil.jpg"},
		{"hello world (1).png", "hello_world_1_.png"},
		{"   ", "image"},
		{"__--..", "image"},
	}

	for _, c := range cases {
		got := SanitizeFileName(c.in)
		if got != c.want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseDataURL(t *testing.T) {
	valid := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	mime, b64, err := ParseDataURL(valid)
	if err != nil {
		t.Fatalf("ParseDataURL failed: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}
	if !strings.HasPrefix(b64, "iVBORw0KGgo") {
		t.Errorf("b64 doesn't match")
	}

	invalid := "data:image/png,notbase64"
	if _, _, err := ParseDataURL(invalid); err == nil {
		t.Error("expected error for invalid data URL")
	}
}

func TestMakeImagePath(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	p := MakeImagePath("myimages", "pic.jpg", "image/jpeg", now)
	if !strings.HasPrefix(p, "myimages/2026/10/03/") {
		t.Errorf("expected date hierarchy in path: %s", p)
	}
	if filepath.Ext(p) != ".jpg" {
		t.Errorf("expected .jpg extension in path: %s", p)
	}
}

func TestConfigAndHistoryStore(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("DS2API_IMAGE_BED_PATH", filepath.Join(tmpDir, "image_bed.json"))
	t.Setenv("DS2API_IMAGE_BED_HISTORY_PATH", filepath.Join(tmpDir, "history.json"))

	cfg := Config{
		Token:      "ghp_test123",
		Owner:      "octocat",
		Repository: "images",
		PathPrefix: "uploads",
		Branch:     "main",
	}

	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if loaded.Owner != "octocat" || loaded.Token != "ghp_test123" {
		t.Errorf("loaded config mismatch: %+v", loaded)
	}

	item := HistoryItem{
		ID:        "item1",
		FileName:  "test.png",
		Path:      "uploads/test.png",
		SHA:       "sha123",
		URL:       "https://raw.githubusercontent.com/octocat/images/main/uploads/test.png",
		Size:      1024,
		CreatedAt: time.Now(),
	}

	if err := AddHistoryItem(item); err != nil {
		t.Fatalf("AddHistoryItem failed: %v", err)
	}

	history, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}
	if len(history) != 1 || history[0].ID != "item1" {
		t.Errorf("history mismatch: %+v", history)
	}

	deleted, err := DeleteHistoryItem("item1")
	if err != nil {
		t.Fatalf("DeleteHistoryItem failed: %v", err)
	}
	if deleted.ID != "item1" {
		t.Errorf("deleted item id = %s, want item1", deleted.ID)
	}

	historyAfter, _ := LoadHistory()
	if len(historyAfter) != 0 {
		t.Errorf("expected empty history after delete, got %d", len(historyAfter))
	}
}

func TestGitHubClientMocked(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/octocat/images"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"private":        false,
				"default_branch": "main",
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": map[string]any{
					"name":         "test.png",
					"path":         "images/test.png",
					"sha":          "mock_sha_123",
					"download_url": "https://raw.githubusercontent.com/octocat/images/main/images/test.png",
				},
			})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/contents/"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"commit": map[string]any{"sha": "commit_sha"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	origBase := githubAPIBase
	githubAPIBase = ts.URL
	defer func() { githubAPIBase = origBase }()

	tmpDir := t.TempDir()
	t.Setenv("DS2API_IMAGE_BED_HISTORY_PATH", filepath.Join(tmpDir, "history.json"))

	cfg := Config{
		Token:      "ghp_fake",
		Owner:      "octocat",
		Repository: "images",
		Branch:     "main",
	}

	ctx := context.Background()
	repo, err := EnsureRepository(ctx, cfg)
	if err != nil {
		t.Fatalf("EnsureRepository failed: %v", err)
	}
	if repo.Private {
		t.Errorf("expected public repo")
	}

	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	item, err := UploadImage(ctx, cfg, dataURL, "sample.png")
	if err != nil {
		t.Fatalf("UploadImage failed: %v", err)
	}
	if item.SHA != "mock_sha_123" {
		t.Errorf("expected SHA mock_sha_123, got %s", item.SHA)
	}

	if err := DeleteRemoteFile(ctx, cfg, item.Path, item.SHA); err != nil {
		t.Fatalf("DeleteRemoteFile failed: %v", err)
	}
}

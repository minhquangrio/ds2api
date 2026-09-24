package usageledger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")

	store1 := New(storePath)
	store1.mu.Lock()
	store1.recordLocked(Record{
		ID:           "rec_rt",
		Model:        "deepseek-chat",
		At:           time.Now().UnixMilli(),
		Status:       "success",
		StatusCode:   200,
		PromptTokens: 50,
		TotalTokens:  50,
	})
	if err := store1.flushLocked(); err != nil {
		store1.mu.Unlock()
		t.Fatalf("flush store1 failed: %v", err)
	}
	store1.mu.Unlock()
	_ = store1.Close()

	// Reload in store2
	store2 := New(storePath)
	defer func() {
		_ = store2.Close()
	}()

	if err := store2.Err(); err != nil {
		t.Fatalf("store2 err: %v", err)
	}

	store2.mu.Lock()
	defer store2.mu.Unlock()
	if store2.totals.Requests != 1 || store2.totals.TotalTokens != 50 {
		t.Fatalf("unexpected reloaded totals: %+v", store2.totals)
	}
	if len(store2.recent) != 1 || store2.recent[0].ID != "rec_rt" {
		t.Fatalf("unexpected reloaded recent: %+v", store2.recent)
	}
}

func TestPersistCorruptedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")

	// Write broken JSON
	if err := os.WriteFile(storePath, []byte("{invalid-json"), 0o644); err != nil {
		t.Fatalf("write broken file failed: %v", err)
	}

	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	if store.Err() == nil {
		t.Fatalf("expected store.Err() to be non-nil for broken JSON")
	}
}

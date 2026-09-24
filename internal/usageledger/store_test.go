package usageledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStoreSingleRecordAllTiers(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	now := time.Now().UnixMilli()
	rec := Record{
		ID:               "rec_1",
		CallerID:         "caller_a",
		AccountID:        "acc_a",
		Model:            "deepseek-chat",
		At:               now,
		Stream:           true,
		Status:           "success",
		StatusCode:       200,
		ElapsedMs:        500,
		PromptTokens:     10,
		CompletionTokens: 20,
		ReasoningTokens:  5,
		TotalTokens:      30,
	}

	store.mu.Lock()
	store.recordLocked(rec)
	store.mu.Unlock()

	// Check totals
	store.mu.Lock()
	if store.totals.Requests != 1 || store.totals.Success != 1 || store.totals.TotalTokens != 30 {
		t.Fatalf("unexpected totals: %+v", store.totals)
	}
	// Check minute bucket
	if len(store.minutes) != 1 {
		t.Fatalf("expected 1 minute bucket, got %d", len(store.minutes))
	}
	expectedMinStart := (now / 60000) * 60000
	if store.minutes[0].StartMs != expectedMinStart || store.minutes[0].TotalTokens != 30 {
		t.Fatalf("unexpected minute bucket: %+v", store.minutes[0])
	}

	// Check hour bucket
	hourShardKey := time.UnixMilli(now).UTC().Format("2006-01")
	hBuckets := store.hours[hourShardKey]
	if len(hBuckets) != 1 {
		t.Fatalf("expected 1 hour bucket, got %d", len(hBuckets))
	}
	expectedHourStart := (now / 3600000) * 3600000
	if hBuckets[0].StartMs != expectedHourStart || hBuckets[0].TotalTokens != 30 {
		t.Fatalf("unexpected hour bucket: %+v", hBuckets[0])
	}
	if hBuckets[0].Models["deepseek-chat"].TotalTokens != 30 {
		t.Fatalf("unexpected hour bucket model counter: %+v", hBuckets[0].Models)
	}

	// Check day bucket
	dayShardKey := time.UnixMilli(now).UTC().Format("2006-01")
	dBuckets := store.days[dayShardKey]
	if len(dBuckets) != 1 {
		t.Fatalf("expected 1 day bucket, got %d", len(dBuckets))
	}
	expectedDayStart := (now / 86400000) * 86400000
	if dBuckets[0].StartMs != expectedDayStart || dBuckets[0].TotalTokens != 30 {
		t.Fatalf("unexpected day bucket: %+v", dBuckets[0])
	}
	store.mu.Unlock()
}

func TestStoreConcurrent200Goroutines(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	var wg sync.WaitGroup
	const goroutines = 200
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rec := Begin(store, Meta{
				Model:    "deepseek-chat",
				CallerID: fmt.Sprintf("caller_%d", id%5),
			})
			rec.Record(Outcome{
				Status:     "success",
				StatusCode: 200,
				Usage: map[string]any{
					"prompt_tokens":     1,
					"completion_tokens": 2,
					"total_tokens":      3,
				},
			})
		}(i)
	}
	wg.Wait()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.totals.Requests != int64(goroutines) {
		t.Fatalf("expected %d requests, got %d", goroutines, store.totals.Requests)
	}
	if store.totals.TotalTokens != int64(goroutines*3) {
		t.Fatalf("expected %d total tokens, got %d", goroutines*3, store.totals.TotalTokens)
	}
}

func TestRecordCalledTwiceOnlyOneRow(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	rec := Begin(store, Meta{
		Model: "deepseek-chat",
	})
	rec.Record(Outcome{Status: "success", StatusCode: 200})
	rec.Record(Outcome{Status: "error", StatusCode: 500})

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.totals.Requests != 1 {
		t.Fatalf("expected exactly 1 request, got %d", store.totals.Requests)
	}
	if store.totals.Success != 1 || store.totals.Errors != 0 {
		t.Fatalf("expected 1 success and 0 errors, got %+v", store.totals)
	}
}

func TestBeginNilStoreReturnsNil(t *testing.T) {
	rec := Begin(nil, Meta{Model: "deepseek-chat"})
	if rec != nil {
		t.Fatalf("expected nil recorder when store is nil")
	}
	// nil receiver safety
	var nilRec *Recorder
	nilRec.Record(Outcome{Status: "success"})
}

func TestPruneRemovesOldBucketsAndDeletesEmptyShardFile(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	recTime := time.Now().Add(-10 * 24 * time.Hour).UnixMilli() // 10 days ago (within 45 days)
	shardKey := time.UnixMilli(recTime).UTC().Format("2006-01")

	store.mu.Lock()
	store.recordLocked(Record{
		ID:           "old_rec",
		Model:        "deepseek-chat",
		At:           recTime,
		Status:       "success",
		StatusCode:   200,
		PromptTokens: 10,
		TotalTokens:  10,
	})
	// Flush to disk so shard file exists
	if err := store.flushLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("flush failed: %v", err)
	}
	store.mu.Unlock()

	shardFile := store.hourShardPath(shardKey)
	if _, err := os.Stat(shardFile); err != nil {
		t.Fatalf("expected shard file to exist: %v", err)
	}

	// Age the bucket past 45 days (60 days ago) and trigger flush
	store.mu.Lock()
	for _, b := range store.hours[shardKey] {
		b.StartMs = time.Now().Add(-60 * 24 * time.Hour).UnixMilli()
	}
	store.dirtyHours[shardKey] = true
	if err := store.flushLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("second flush failed: %v", err)
	}
	store.mu.Unlock()

	if _, err := os.Stat(shardFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected empty hour shard file to be deleted, got err: %v", err)
	}
}

func TestStoreCallersTrackingAndCallerTotalTokens(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() { _ = store.Close() }()

	store.mu.Lock()
	store.recordLocked(Record{
		CallerID:    "caller_1",
		TotalTokens: 150,
		Status:      "success",
	})
	store.recordLocked(Record{
		CallerID:    "caller_1",
		TotalTokens: 250,
		Status:      "success",
	})
	store.recordLocked(Record{
		CallerID:    "caller_2",
		TotalTokens: 100,
		Status:      "success",
	})
	store.recordLocked(Record{
		CallerID:    "", // anonymous
		TotalTokens: 50,
		Status:      "success",
	})
	store.mu.Unlock()

	if tokens := store.CallerTotalTokens("caller_1"); tokens != 400 {
		t.Errorf("expected 400 tokens for caller_1, got %d", tokens)
	}
	if tokens := store.CallerTotalTokens("caller_2"); tokens != 100 {
		t.Errorf("expected 100 tokens for caller_2, got %d", tokens)
	}
	if tokens := store.CallerTotalTokens("unknown_caller"); tokens != 0 {
		t.Errorf("expected 0 tokens for unknown caller, got %d", tokens)
	}
	if tokens := store.CallerTotalTokens(""); tokens != 0 {
		t.Errorf("expected 0 tokens for empty caller ID, got %d", tokens)
	}

	var nilStore *Store
	if tokens := nilStore.CallerTotalTokens("caller_1"); tokens != 0 {
		t.Errorf("expected 0 tokens on nil store, got %d", tokens)
	}
}

func TestStoreCallersPersistAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store1 := New(storePath)

	store1.mu.Lock()
	store1.recordLocked(Record{
		CallerID:    "user_abc",
		TotalTokens: 1234,
		Status:      "success",
	})
	if err := store1.flushLocked(); err != nil {
		store1.mu.Unlock()
		t.Fatalf("flush failed: %v", err)
	}
	store1.mu.Unlock()
	_ = store1.Close()

	// Load in a fresh store instance
	store2 := New(storePath)
	defer func() { _ = store2.Close() }()

	store2.mu.Lock()
	if err := store2.loadLocked(); err != nil {
		store2.mu.Unlock()
		t.Fatalf("load failed: %v", err)
	}
	store2.mu.Unlock()

	if tokens := store2.CallerTotalTokens("user_abc"); tokens != 1234 {
		t.Errorf("expected 1234 tokens after reload, got %d", tokens)
	}
}

func TestStoreLoadLegacyFileWithoutCallers(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")

	// Write old payload without callers field
	legacyJSON := `{
		"version": 1,
		"revision": 5,
		"flushed_at": 1715635200000,
		"totals": {"requests": 10, "total_tokens": 5000}
	}`
	if err := os.WriteFile(storePath, []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("failed to write legacy ledger: %v", err)
	}

	store := New(storePath)
	defer func() { _ = store.Close() }()

	store.mu.Lock()
	if err := store.loadLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("loadLocked failed on legacy json: %v", err)
	}
	store.mu.Unlock()

	if store.totals.TotalTokens != 5000 {
		t.Errorf("expected totals.TotalTokens=5000, got %d", store.totals.TotalTokens)
	}
	if tokens := store.CallerTotalTokens("any_user"); tokens != 0 {
		t.Errorf("expected 0 tokens on legacy ledger, got %d", tokens)
	}
}

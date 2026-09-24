package usageledger

import (
	"path/filepath"
	"testing"

	"ds2api/internal/chathistory"
)

func TestBackfillFromHistory(t *testing.T) {
	dir := t.TempDir()
	histPath := filepath.Join(dir, "chat_history.json")
	histStore := chathistory.New(histPath)

	// Create 3 history entries
	e1, err := histStore.Start(chathistory.StartParams{
		Model:     "deepseek-chat",
		CallerID:  "caller_1",
		UserInput: "first question",
	})
	if err != nil {
		t.Fatalf("start e1 failed: %v", err)
	}
	_, err = histStore.Update(e1.ID, chathistory.UpdateParams{
		Status:    "success",
		Content:   "answer 1",
		Completed: true,
		Usage: map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		},
	})
	if err != nil {
		t.Fatalf("update e1 failed: %v", err)
	}

	e2, err := histStore.Start(chathistory.StartParams{
		Model:     "deepseek-chat",
		CallerID:  "caller_2",
		UserInput: "second question",
	})
	if err != nil {
		t.Fatalf("start e2 failed: %v", err)
	}
	_, err = histStore.Update(e2.ID, chathistory.UpdateParams{
		Status:    "error",
		Error:     "rate limit",
		Completed: true,
		Usage: map[string]any{
			"prompt_tokens":     5,
			"completion_tokens": 0,
			"total_tokens":      5,
		},
	})
	if err != nil {
		t.Fatalf("update e2 failed: %v", err)
	}

	e3, err := histStore.Start(chathistory.StartParams{
		Model:     "deepseek-reasoner",
		CallerID:  "caller_1",
		UserInput: "third question",
	})
	if err != nil {
		t.Fatalf("start e3 failed: %v", err)
	}
	_, err = histStore.Update(e3.ID, chathistory.UpdateParams{
		Status:           "success",
		ReasoningContent: "thinking",
		Content:          "answer 3",
		Completed:        true,
		Usage: map[string]any{
			"prompt_tokens":     15,
			"completion_tokens": 25,
			"reasoning_tokens":  10,
			"total_tokens":      40,
		},
	})
	if err != nil {
		t.Fatalf("update e3 failed: %v", err)
	}

	snap, err := histStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	var expectedTokens int64
	for _, it := range snap.Items {
		expectedTokens += int64(it.TotalTokens)
	}

	// Now create usageledger store and backfill
	ledgerPath := filepath.Join(dir, "usage_ledger.json")
	ledger := New(ledgerPath)
	defer func() {
		_ = ledger.Close()
	}()

	if err := ledger.BackfillFromHistory(histStore); err != nil {
		t.Fatalf("backfill failed: %v", err)
	}

	ledger.mu.Lock()
	if ledger.totals.Requests != 3 {
		t.Fatalf("expected 3 requests, got %d", ledger.totals.Requests)
	}
	if ledger.totals.TotalTokens != expectedTokens {
		t.Fatalf("expected total tokens %d, got %d", expectedTokens, ledger.totals.TotalTokens)
	}
	if !ledger.backfill.Done || ledger.backfill.Entries != 3 {
		t.Fatalf("unexpected backfill marker: %+v", ledger.backfill)
	}
	ledger.mu.Unlock()

	// Running backfill again should be idempotent and change nothing
	if err := ledger.BackfillFromHistory(histStore); err != nil {
		t.Fatalf("second backfill failed: %v", err)
	}

	ledger.mu.Lock()
	if ledger.totals.Requests != 3 || ledger.totals.TotalTokens != expectedTokens {
		t.Fatalf("expected totals unchanged after second backfill, got %+v", ledger.totals)
	}
	ledger.mu.Unlock()
}

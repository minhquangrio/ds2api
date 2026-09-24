package usageledger

import (
	"path/filepath"
	"testing"

	"ds2api/internal/chathistory"
)

func TestRecorderInfersUsageWhenNil(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	rec := Begin(store, Meta{
		Model:  "deepseek-chat",
		Prompt: "What is the capital of France?",
	})
	rec.Record(Outcome{
		Status:     "success",
		StatusCode: 200,
		Thinking:   "Thinking about France",
		Content:    "The capital of France is Paris.",
		Usage:      nil, // nil => should infer
	})

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.totals.Requests != 1 {
		t.Fatalf("expected 1 request, got %d", store.totals.Requests)
	}
	if store.totals.PromptTokens == 0 || store.totals.CompletionTokens == 0 || store.totals.TotalTokens == 0 {
		t.Fatalf("expected non-zero inferred tokens, got %+v", store.totals)
	}
}

func TestExtractTokenCountsCompatibility(t *testing.T) {
	map1 := map[string]any{
		"prompt_tokens":     15,
		"completion_tokens": 25,
		"total_tokens":      40,
	}
	map2 := map[string]any{
		"input_tokens":  15,
		"output_tokens": 25,
		"total_tokens":  40,
	}

	p1, c1, _, t1 := chathistory.ExtractTokenCounts(map1)
	p2, c2, _, t2 := chathistory.ExtractTokenCounts(map2)

	if p1 != p2 || c1 != c2 || t1 != t2 {
		t.Fatalf("expected equal token counts, got (%d,%d,%d) vs (%d,%d,%d)", p1, c1, t1, p2, c2, t2)
	}
}

func TestRecorderErrorStatusWithEmptyText(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	rec := Begin(store, Meta{
		Model:  "deepseek-chat",
		Prompt: "",
	})
	rec.Record(Outcome{
		Status:     "error",
		StatusCode: 500,
		Thinking:   "",
		Content:    "",
		Usage:      nil,
	})

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.totals.Requests != 1 || store.totals.Errors != 1 || store.totals.Success != 0 {
		t.Fatalf("expected 1 request, 1 error, got %+v", store.totals)
	}
	if store.totals.TotalTokens != 0 {
		t.Fatalf("expected 0 tokens for empty error, got %d", store.totals.TotalTokens)
	}
}

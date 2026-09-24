package responsehistory

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
	"ds2api/internal/promptcompat"
	"ds2api/internal/usageledger"
)

func TestProgressPersistDueCoalescesFrequentUpdates(t *testing.T) {
	base := time.Unix(100, 0)
	if progressPersistDue(base.Add(500*time.Millisecond), base) {
		t.Fatal("expected frequent progress update to be coalesced")
	}
	if !progressPersistDue(base.Add(time.Second), base) {
		t.Fatal("expected progress update after one second")
	}
}

func TestProgressPersistDueAllowsInitialUpdate(t *testing.T) {
	if !progressPersistDue(time.Unix(100, 0), time.Time{}) {
		t.Fatal("expected initial progress update")
	}
}

func TestSessionWithDisabledChatHistoryStillRecordsToLedger(t *testing.T) {
	dir := t.TempDir()
	histStore := chathistory.New(filepath.Join(dir, "chat_history.json"))
	if _, err := histStore.SetLimit(0); err != nil {
		t.Fatalf("set limit 0 failed: %v", err)
	}
	if histStore.Enabled() {
		t.Fatalf("expected histStore.Enabled() == false")
	}

	ledger := usageledger.New(filepath.Join(dir, "usage_ledger.json"))
	defer func() { _ = ledger.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	authData := &auth.RequestAuth{CallerID: "caller_1", AccountID: "acc_1"}

	sess := Start(StartParams{
		Store:   histStore,
		Ledger:  ledger,
		Request: req,
		Auth:    authData,
		Surface: "claude.messages",
		Standard: promptcompat.StandardRequest{
			ResponseModel: "deepseek-chat",
			FinalPrompt:   "Hello",
		},
	})
	if sess == nil {
		t.Fatalf("expected non-nil session even when history is disabled")
	}

	sess.Success(200, "thinking", "answer", "stop", map[string]any{
		"input_tokens":  5,
		"output_tokens": 10,
		"total_tokens":  15,
	})

	res, err := ledger.Query(usageledger.QueryOptions{RangeKey: "all"})
	if err != nil {
		t.Fatalf("query ledger failed: %v", err)
	}
	if res.Totals.Requests != 1 || res.Totals.Success != 1 || res.Totals.TotalTokens != 15 {
		t.Fatalf("unexpected ledger totals: %+v", res.Totals)
	}
}

func TestSessionStreamPrepareCreatesNoRecords(t *testing.T) {
	dir := t.TempDir()
	ledger := usageledger.New(filepath.Join(dir, "usage_ledger.json"))
	defer func() { _ = ledger.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages?__stream_prepare=1", nil)
	authData := &auth.RequestAuth{CallerID: "caller_1"}

	sess := Start(StartParams{
		Ledger:  ledger,
		Request: req,
		Auth:    authData,
		Surface: "claude.messages",
	})
	if sess != nil {
		t.Fatalf("expected nil session for stream prepare request")
	}

	res, err := ledger.Query(usageledger.QueryOptions{RangeKey: "all"})
	if err != nil {
		t.Fatalf("query ledger failed: %v", err)
	}
	if res.Totals.Requests != 0 {
		t.Fatalf("expected 0 requests in ledger, got %d", res.Totals.Requests)
	}
}

func TestSessionErrorCreatesErrorRecord(t *testing.T) {
	dir := t.TempDir()
	ledger := usageledger.New(filepath.Join(dir, "usage_ledger.json"))
	defer func() { _ = ledger.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	authData := &auth.RequestAuth{CallerID: "caller_1"}

	sess := Start(StartParams{
		Ledger:  ledger,
		Request: req,
		Auth:    authData,
		Surface: "claude.messages",
		Standard: promptcompat.StandardRequest{
			ResponseModel: "deepseek-chat",
			FinalPrompt:   "Hello",
		},
	})
	if sess == nil {
		t.Fatalf("expected non-nil session")
	}

	sess.Error(500, "upstream timeout", "error", "", "")

	res, err := ledger.Query(usageledger.QueryOptions{RangeKey: "all"})
	if err != nil {
		t.Fatalf("query ledger failed: %v", err)
	}
	if res.Totals.Requests != 1 || res.Totals.Errors != 1 || res.Totals.Success != 0 {
		t.Fatalf("unexpected ledger totals: %+v", res.Totals)
	}
	if len(res.Recent) != 1 || res.Recent[0].Status != "error" {
		t.Fatalf("unexpected recent status: %+v", res.Recent)
	}
}

package codex

import (
	"testing"
)

func TestStreamParserEvents(t *testing.T) {
	parser := NewStreamParser()

	chunk := []byte("event: response.reasoning.delta\ndata: {\"delta\":\"Thinking...\"}\n\n" +
		"event: response.text.delta\ndata: {\"delta\":\"Hello world!\"}\n\n" +
		"event: response.function_call_arguments.delta\ndata: {\"call_id\":\"call_1\",\"delta\":\"{\\\"query\\\":\\\"go\\\"}\"}\n\n" +
		"event: response.completed\ndata: {\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":20,\"total_tokens\":30}}}\n\n" +
		"data: [DONE]\n\n")

	events := parser.Feed(chunk)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	// 1. Reasoning delta
	if rsn, ok := events[0].ParseReasoningDelta(); !ok || rsn != "Thinking..." {
		t.Errorf("reasoning delta mismatch: %s, %v", rsn, ok)
	}

	// 2. Text delta
	if txt, ok := events[1].ParseTextDelta(); !ok || txt != "Hello world!" {
		t.Errorf("text delta mismatch: %s, %v", txt, ok)
	}

	// 3. Function call delta
	if callID, delta, ok := events[2].ParseFunctionCallArgumentsDelta(); !ok || callID != "call_1" || delta != `{"query":"go"}` {
		t.Errorf("func call delta mismatch: %s, %s, %v", callID, delta, ok)
	}

	// 4. Completed usage
	if in, out, tot, ok := events[3].ParseCompletedUsage(); !ok || in != 10 || out != 20 || tot != 30 {
		t.Errorf("usage mismatch: in=%d, out=%d, tot=%d, ok=%v", in, out, tot, ok)
	}

	// 5. Done
	if term, ok := events[4].ParseTerminal(); !ok || term.Type != "[DONE]" {
		t.Errorf("expected terminal [DONE], got %+v (ok=%v)", term, ok)
	}
}

// TestParseTerminalReportsIncompleteAndFailed guards against treating a
// truncated or errored upstream turn as a clean finish.
func TestParseTerminalReportsIncompleteAndFailed(t *testing.T) {
	incomplete := Event{Type: "response.incomplete", Data: `{"response":{"usage":{"input_tokens":5,"output_tokens":7}}}`}
	term, ok := incomplete.ParseTerminal()
	if !ok || term.Type != "response.incomplete" || term.Failed {
		t.Fatalf("unexpected terminal: %+v (ok=%v)", term, ok)
	}
	if term.InputTokens != 5 || term.OutputTokens != 7 || term.TotalTokens != 12 {
		t.Errorf("usage not captured: %+v", term)
	}

	failed := Event{Type: "response.failed", Data: `{"response":{"error":{"message":"upstream exploded"}}}`}
	term, ok = failed.ParseTerminal()
	if !ok || !term.Failed || term.ErrorMessage != "upstream exploded" {
		t.Fatalf("unexpected terminal: %+v (ok=%v)", term, ok)
	}
}

// TestToolCallTrackerAssignsDistinctIndices guards the parallel tool-call case
// that the previous hard-coded index 0 got wrong.
func TestToolCallTrackerAssignsDistinctIndices(t *testing.T) {
	tracker := NewToolCallTracker()
	if got := tracker.IndexFor("call_a"); got != 0 {
		t.Errorf("first call index = %d; want 0", got)
	}
	if got := tracker.IndexFor("call_b"); got != 1 {
		t.Errorf("second call index = %d; want 1", got)
	}
	// Re-seeing a call must not allocate a new index.
	if got := tracker.IndexFor("call_a"); got != 0 {
		t.Errorf("repeat call index = %d; want 0", got)
	}
	if tracker.Len() != 2 {
		t.Errorf("tracker len = %d; want 2", tracker.Len())
	}
}

func TestStreamParserChunkSplits(t *testing.T) {
	parser := NewStreamParser()

	part1 := []byte("event: response.text.delta\nda")
	part2 := []byte("ta: {\"delta\":\"Chunked message\"}\n\n")

	ev1 := parser.Feed(part1)
	if len(ev1) != 0 {
		t.Errorf("expected 0 events from incomplete chunk, got %d", len(ev1))
	}

	ev2 := parser.Feed(part2)
	if len(ev2) != 1 {
		t.Fatalf("expected 1 event after complete chunk, got %d", len(ev2))
	}

	if txt, ok := ev2[0].ParseTextDelta(); !ok || txt != "Chunked message" {
		t.Errorf("text delta mismatch: %s", txt)
	}
}

package muse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

func TestClientHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "secret", "", config.Account{Name: "test"})
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
}

func TestClientExecuteChat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":        "ok",
			"text":          "Hello from mock worker",
			"reasoning":     "thought process",
			"finish_reason": "stop",
			"usage": map[string]int{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "secret", "", config.Account{Name: "test", Cookies: "hatch_sess=123"})
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "muse-spark",
		FinalPrompt:   "Hello",
		Thinking:      true,
	}

	turn, err := client.ExecuteChat(context.Background(), stdReq)
	if err != nil {
		t.Fatalf("ExecuteChat failed: %v", err)
	}

	if turn.Text != "Hello from mock worker" {
		t.Errorf("got text %q, want 'Hello from mock worker'", turn.Text)
	}
	if turn.Thinking != "thought process" {
		t.Errorf("got thinking %q, want 'thought process'", turn.Thinking)
	}
	if turn.Usage.TotalTokens != 15 {
		t.Errorf("got total tokens %d, want 15", turn.Usage.TotalTokens)
	}
}

func TestClientStreamChat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		_, _ = fmt.Fprint(w, "event: delta\ndata: {\"text_delta\":\"Hello \",\"reasoning_delta\":\"think \"}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "event: delta\ndata: {\"text_delta\":\"world!\"}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "event: completed\ndata: {\"finish_reason\":\"stop\",\"text\":\"Hello world!\"}\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", "", config.Account{Name: "test", Cookies: "hatch_sess=123"})
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "muse-spark",
		FinalPrompt:   "Hello",
		Thinking:      true,
	}

	var textDeltas []string
	turn, err := client.StreamChat(context.Background(), stdReq, func(textDelta, reasoningDelta string) error {
		if textDelta != "" {
			textDeltas = append(textDeltas, textDelta)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("StreamChat failed: %v", err)
	}

	if turn.Text != "Hello world!" {
		t.Errorf("got turn.Text %q, want 'Hello world!'", turn.Text)
	}
	if strings.Join(textDeltas, "") != "Hello world!" {
		t.Errorf("deltas joined %q, want 'Hello world!'", strings.Join(textDeltas, ""))
	}
}

func TestStreamOpenAIChat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		_, _ = fmt.Fprint(w, "event: delta\ndata: {\"text_delta\":\"Hi \"}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "event: delta\ndata: {\"text_delta\":\"there\"}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "event: completed\ndata: {\"finish_reason\":\"stop\"}\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", "", config.Account{Name: "test", Cookies: "hatch_sess=123"})
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "muse-spark",
		ResponseModel: "muse-spark",
		FinalPrompt:   "Hi",
	}

	recorder := httptest.NewRecorder()
	_, text, err := StreamOpenAIChat(context.Background(), client, stdReq, recorder)
	if err != nil {
		t.Fatalf("StreamOpenAIChat failed: %v", err)
	}

	if text != "Hi there" {
		t.Errorf("got text %q, want 'Hi there'", text)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "chat.completion.chunk") {
		t.Errorf("expected chunk in SSE, got %s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Errorf("expected [DONE] in SSE, got %s", body)
	}
}

func TestStreamResponsesAPI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		_, _ = fmt.Fprint(w, "event: delta\ndata: {\"text_delta\":\"Responses delta\"}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "event: completed\ndata: {\"finish_reason\":\"stop\"}\n\n")
		flusher.Flush()
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", "", config.Account{Name: "test", Cookies: "hatch_sess=123"})
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "muse-spark",
		ResponseModel: "muse-spark",
		FinalPrompt:   "Hi",
	}

	recorder := httptest.NewRecorder()
	err := StreamResponsesAPI(context.Background(), client, stdReq, recorder)
	if err != nil {
		t.Fatalf("StreamResponsesAPI failed: %v", err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "response.created") {
		t.Errorf("expected response.created in SSE, got %s", body)
	}
	if !strings.Contains(body, "response.completed") {
		t.Errorf("expected response.completed in SSE, got %s", body)
	}
}

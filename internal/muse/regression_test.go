package muse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ds2api/internal/assistantturn"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

const toolFixture = `<tool_calls><invoke name="read_file"><parameter name="path">test.txt</parameter></invoke></tool_calls>`

func fixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.URL, "", "", config.Account{Name: "fixture", Provider: "muse", Cookies: "hatch_sess=fixture"})
}

func completedFixture(w http.ResponseWriter, text, reasoning string) {
	body, _ := json.Marshal(map[string]any{"text": text, "reasoning": reasoning, "finish_reason": "stop"})
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "event: completed\ndata: %s\n\n", body)
}

func toolRequest() promptcompat.StandardRequest {
	return promptcompat.StandardRequest{
		ResolvedModel: ModelMuseSpark, ResponseModel: ModelMuseSpark, FinalPrompt: "read test.txt",
		ToolNames: []string{"read_file"},
		ToolsRaw: []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "read_file", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		}}},
		ToolChoice: promptcompat.ToolChoicePolicy{Mode: promptcompat.ToolChoiceRequired},
	}
}

func TestStreamToolsRenderedAcrossProtocols(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { completedFixture(w, toolFixture, "") })
	for _, test := range []struct {
		name, expected string
		render         StreamRenderer
	}{
		{"chat", `"tool_calls"`, StreamOpenAIChat},
		{"claude", `"tool_use"`, StreamClaudeMessages},
		{"gemini", `"functionCall"`, StreamGeminiContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			_, _, err := test.render(context.Background(), client, toolRequest(), recorder)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(recorder.Body.String(), test.expected) || strings.Contains(recorder.Body.String(), "<tool_calls>") {
				t.Fatalf("tool call not rendered as %s: %s", test.expected, recorder.Body.String())
			}
		})
	}
}

func TestStreamTerminalValidationAndLargeOutput(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantError  bool
	}{
		{"partial EOF", "event: delta\ndata: {\"text_delta\":\"partial\"}\n\n", true},
		{"malformed completed", "event: delta\ndata: {\"text_delta\":\"partial\"}\n\nevent: completed\ndata: {bad}\n\n", true},
		{"large completed", "event: completed\ndata: {\"text\":\"" + strings.Repeat("x", 70000) + "\"}\n\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, test.body) })
			recorder := httptest.NewRecorder()
			_, text, err := StreamOpenAIChat(context.Background(), client, promptcompat.StandardRequest{ResponseModel: ModelMuseSpark}, recorder)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v", err)
			}
			if test.wantError && (text != "partial" || strings.Contains(recorder.Body.String(), "[DONE]")) {
				t.Fatalf("partial stream incorrectly finalized: %s", recorder.Body.String())
			}
			if !test.wantError && !strings.Contains(recorder.Body.String(), strings.Repeat("x", 70000)) {
				t.Fatal("completed-only text missing")
			}
		})
	}
}

func TestDisabledThinkingStillDetectsTools(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "", "reasoning": toolFixture, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 12, "reasoning_tokens": 7}})
	})
	turn, err := client.ExecuteChat(context.Background(), toolRequest())
	if err != nil {
		t.Fatal(err)
	}
	if turn.Thinking != "" || turn.RawThinking != toolFixture || len(turn.ToolCalls) != 1 {
		t.Fatalf("lost raw reasoning or exposed it: %+v", turn)
	}
	if turn.Usage.TotalTokens != 22 {
		t.Fatalf("reasoning counted twice: %+v", turn.Usage)
	}
}

func TestEmptyRetryAndContentFilter(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, filtered := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/filter=%v", stream, filtered), func(t *testing.T) {
				calls := 0
				client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					var payload map[string]any
					_ = json.NewDecoder(r.Body).Decode(&payload)
					text, finish := "", "stop"
					if filtered {
						finish = "content_filter"
					} else if calls > 1 {
						text = "retried"
						if !strings.Contains(fmt.Sprint(payload["prompt"]), "Previous reply had no visible output") {
							t.Error("retry suffix missing")
						}
					}
					result := map[string]any{"text": text, "finish_reason": finish}
					if stream {
						body, _ := json.Marshal(result)
						_, _ = fmt.Fprintf(w, "event: completed\ndata: %s\n\n", body)
					} else {
						_ = json.NewEncoder(w).Encode(result)
					}
				})
				var turn assistantturn.Turn
				var err error
				if stream {
					turn, err = client.StreamChat(context.Background(), promptcompat.StandardRequest{FinalPrompt: "hello"}, nil)
				} else {
					turn, err = ExecuteTurn(context.Background(), client, promptcompat.StandardRequest{FinalPrompt: "hello"})
				}
				if filtered {
					if calls != 1 || err == nil {
						t.Fatalf("filtered output retried: calls=%d err=%v", calls, err)
					}
				} else if calls != 2 || err != nil || turn.Text != "retried" {
					t.Fatalf("retry failed: calls=%d turn=%+v err=%v", calls, turn, err)
				}
			})
		}
	}
}

func TestWorkerModelStripsDisplayVariant(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["model"] != "muse-spark" || payload["prompt"] != "fallback prompt" {
			t.Errorf("bad worker payload: %+v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "answer"})
	})
	turn, err := client.ExecuteChat(context.Background(), promptcompat.StandardRequest{RequestedModel: "muse-spark-nothinking", PromptTokenText: "fallback prompt"})
	if err != nil || turn.Prompt != "fallback prompt" {
		t.Fatalf("fallback/variant failed: turn=%+v err=%v", turn, err)
	}
}

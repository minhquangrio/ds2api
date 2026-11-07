package muse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/assistantturn"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

func TestStreamingAccountRetryOnlyBeforeOutput(t *testing.T) {
	for _, renderer := range []struct {
		name   string
		render TurnStreamRenderer
	}{
		{"chat", StreamOpenAIChatWithTurn}, {"claude", StreamClaudeMessagesWithTurn}, {"gemini", StreamGeminiContentWithTurn},
		{"responses", func(ctx context.Context, c *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (assistantturn.Turn, error) {
			turn, _, err := StreamResponsesWithID(ctx, c, req, w, "resp_fixture")
			return turn, err
		}},
	} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/partial=%v", renderer.name, partial), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if calls == 1 {
						if !partial {
							w.WriteHeader(http.StatusTooManyRequests)
							return
						}
						_, _ = fmt.Fprint(w, "event: delta\ndata: {\"text_delta\":\"first answer\"}\n\nevent: error\ndata: {\"status\":429,\"error\":\"rate limit\"}\n\n")
						return
					}
					completedFixture(w, "second answer", "")
				}))
				defer server.Close()
				t.Setenv("DS2API_MUSE_WORKER_URL", server.URL)
				t.Setenv("DS2API_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
				t.Setenv("DS2API_CONFIG_JSON", `{"keys":["fixture-key"],"accounts":[{"name":"m1","provider":"muse","cookies":"hatch_sess=one"},{"name":"m2","provider":"muse","cookies":"hatch_sess=two"}]}`)
				store := config.LoadStore()
				resolver := auth.NewResolver(store, account.NewPool(store), func(context.Context, config.Account) (string, error) {
					t.Fatal("DeepSeek login called")
					return "", nil
				})
				r := httptest.NewRequest("POST", "/", nil)
				r.Header.Set("Authorization", "Bearer fixture-key")
				a, err := resolver.DetermineForProvider(r, "muse")
				if err != nil {
					t.Fatal(err)
				}
				defer resolver.Release(a)
				recorder := httptest.NewRecorder()
				turn, err := ExecuteStreamTurnWithRetry(r.Context(), a, store, promptcompat.StandardRequest{ResolvedModel: ModelMuseSpark, ResponseModel: ModelMuseSpark}, recorder, renderer.render, nil)
				if partial {
					if calls != 1 || err == nil || !StreamCommitted(err) || turn.Text != "first answer" || strings.Contains(recorder.Body.String(), "second answer") {
						t.Fatalf("retried committed stream: calls=%d err=%v text=%q", calls, err, turn.Text)
					}
				} else if calls != 2 || err != nil || turn.Text != "second answer" {
					t.Fatalf("pre-output retry failed: calls=%d err=%v text=%q", calls, err, turn.Text)
				}
			})
		}
	}
}

func TestResponsesCompletedUsesStableItems(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { completedFixture(w, "answer", "thinking") })
	rec := httptest.NewRecorder()
	_, obj, err := StreamResponsesWithID(context.Background(), client, promptcompat.StandardRequest{ResponseModel: ModelMuseSpark, Thinking: true}, rec, "resp_fixture")
	if err != nil {
		t.Fatal(err)
	}
	items := obj["output"].([]any)
	if obj["id"] != "resp_fixture" || items[0].(map[string]any)["id"] != "msg_resp_fixture" {
		t.Fatalf("unstable response/item IDs: %+v", obj)
	}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			var event map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			if event["type"] == nil {
				t.Fatal("missing event discriminator")
			}
			if event["type"] == "response.created" && event["response"] == nil {
				t.Fatal("created has no response")
			}
			if event["type"] == "response.completed" {
				response := event["response"].(map[string]any)
				if response["output"] == nil || response["usage"] == nil {
					t.Fatal("incomplete completed response")
				}
			}
		}
	}
}

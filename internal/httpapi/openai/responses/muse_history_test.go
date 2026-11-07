package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
	"ds2api/internal/config"
	"github.com/go-chi/chi/v5"
)

func TestMuseResponsesStreamRetrievalAndHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "event: delta\ndata: {\"text_delta\":\"stored answer\"}\n\nevent: completed\ndata: {\"text\":\"stored answer\",\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4}}\n\n")
	}))
	defer server.Close()
	t.Setenv("DS2API_MUSE_WORKER_URL", server.URL)
	t.Setenv("DS2API_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["fixture-key"],"accounts":[{"name":"muse-fixture","provider":"muse","cookies":"hatch_sess=fixture"}]}`)
	store := config.LoadStore()
	resolver := auth.NewResolver(store, account.NewPool(store), func(context.Context, config.Account) (string, error) {
		t.Fatal("DeepSeek login called")
		return "", nil
	})
	history := chathistory.New(filepath.Join(t.TempDir(), "history.json"))
	h := &Handler{Store: store, Auth: resolver, ChatHistory: history}
	router := chi.NewRouter()
	RegisterRoutes(router, h)
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"muse-spark","input":"hello","stream":true}`))
	req.Header.Set("Authorization", "Bearer fixture-key")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var completed map[string]any
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			var event map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			if event["type"] == "response.completed" {
				completed = event["response"].(map[string]any)
			}
		}
	}
	if completed == nil {
		t.Fatalf("missing completed: %s", rec.Body.String())
	}
	get := httptest.NewRequest("GET", "/v1/responses/"+completed["id"].(string), nil)
	get.Header.Set("Authorization", "Bearer fixture-key")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, get)
	var stored map[string]any
	if err := json.Unmarshal(result.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if result.Code != 200 || !reflect.DeepEqual(stored, completed) {
		t.Fatalf("retrieved object differs: %d %s", result.Code, result.Body.String())
	}
	snapshot, err := history.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("history entries=%d", len(snapshot.Items))
	}
	item, err := history.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Content != "stored answer" {
		t.Fatalf("missing history output: %+v", item)
	}
}

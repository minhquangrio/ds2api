package videos

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
	"ds2api/internal/config"
	"ds2api/internal/mediacompat"

	"github.com/go-chi/chi/v5"
)

type fakeWorker struct {
	t       *testing.T
	mu      sync.Mutex
	calls   []call
	replies map[string]reply
}

type call struct {
	method  string
	path    string
	query   string
	payload map[string]any
	raw     []byte
}

type reply struct {
	status      int
	body        any
	contentType string
	bytes       []byte
}

func (f *fakeWorker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read body: %v", err)
		return
	}
	key := r.Method + " " + r.URL.Path
	payload := map[string]any{}
	if len(body) > 0 && strings.Contains(r.Header.Get("Content-Type"), "json") {
		if err := json.Unmarshal(body, &payload); err != nil {
			f.t.Errorf("worker body is not json: %v (%s)", err, body)
			return
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, payload: payload, raw: body})
	respond, found := f.replies[key]
	f.mu.Unlock()

	if !found {
		respond = f.defaultReply(r.Method, r.URL.Path)
	}
	if respond.bytes != nil {
		w.Header().Set("Content-Type", respond.contentType)
		w.WriteHeader(respond.status)
		_, _ = w.Write(respond.bytes)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(respond.status)
	_ = json.NewEncoder(w).Encode(respond.body)
}

func (f *fakeWorker) defaultReply(method, path string) reply {
	switch {
	case method == http.MethodPost && path == "/v1/videos":
		return reply{status: http.StatusOK, body: map[string]any{"status": "ok", "task": map[string]any{
			"id": "vid_1", "status": "queued", "stage": "queued", "progress": 0, "model": "muse-video",
			"duration": 6, "created_at": 1735689600,
		}}}
	case method == http.MethodGet && path == "/v1/videos/vid_1":
		return reply{status: http.StatusOK, body: map[string]any{"status": "ok", "task": map[string]any{
			"id": "vid_1", "status": "completed", "stage": "completed", "progress": 100, "model": "muse-video",
			"media_id": "med_1", "content_type": "video/mp4", "size_bytes": 11, "created_at": 1735689600, "updated_at": 1735689700,
		}}}
	case method == http.MethodGet && path == "/v1/videos/vid_1/content":
		return reply{status: http.StatusOK, contentType: "video/mp4", bytes: []byte("video-payload")}
	case method == http.MethodGet && path == "/v1/videos":
		return reply{status: http.StatusOK, body: map[string]any{"status": "ok", "data": []any{
			map[string]any{"id": "vid_1", "status": "completed", "model": "muse-video"},
		}}}
	default:
		return reply{status: http.StatusNotFound, body: map[string]any{"error": "video job not found", "code": "not_found"}}
	}
}

func (f *fakeWorker) lastCall() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return call{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeWorker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func newTestHandler(t *testing.T, worker http.Handler) (*Handler, *chi.Mux, *chathistory.Store) {
	t.Helper()
	server := httptest.NewServer(worker)
	t.Cleanup(server.Close)
	t.Setenv("DS2API_MUSE_WORKER_URL", server.URL)
	t.Setenv("DS2API_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["fixture-key"],"accounts":[{"name":"muse-fixture","provider":"muse","cookies":"hatch_sess=fixture"}]}`)
	store := config.LoadStore()
	resolver := auth.NewResolver(store, account.NewPool(store), func(context.Context, config.Account) (string, error) {
		t.Fatal("DeepSeek login must not run for muse media")
		return "", nil
	})
	history := chathistory.New(filepath.Join(t.TempDir(), "history.json"))
	handler := &Handler{Store: store, Auth: resolver, ChatHistory: history}
	router := chi.NewRouter()
	RegisterRoutes(router, handler)
	return handler, router, history
}

func jsonRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer fixture-key")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestCreateReturnsTaskAndForwardsCaller(t *testing.T) {
	worker := &fakeWorker{t: t}
	_, router, history := newTestHandler(t, worker)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, jsonRequest("/v1/videos", `{"model":"muse-video","prompt":"a slow orbit","seconds":"6"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["id"] != "vid_1" || body["status"] != "queued" || body["object"] != "video" {
		t.Fatalf("task body=%+v", body)
	}
	if body["seconds"] != "6" {
		t.Fatalf("seconds echo=%v", body["seconds"])
	}
	call := worker.lastCall()
	if call.payload["caller_id"] == nil || call.payload["caller_id"] == "" {
		t.Fatalf("caller id must be forwarded for ownership: %+v", call.payload)
	}
	if call.payload["prompt"] != "a slow orbit" || call.payload["duration"] != float64(6) {
		t.Fatalf("payload=%+v", call.payload)
	}
	snapshot, err := history.Snapshot()
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].Surface != "openai.videos_create" {
		t.Fatalf("history=%+v err=%v", snapshot.Items, err)
	}
}

func TestCreateMultipartReference(t *testing.T) {
	worker := &fakeWorker{t: t}
	_, router, _ := newTestHandler(t, worker)

	var builder strings.Builder
	writer := multipart.NewWriter(&builder)
	_ = writer.WriteField("model", "muse-video")
	_ = writer.WriteField("prompt", "animate this")
	part, err := writer.CreateFormFile("input_reference", "frame.png")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\npixels"))
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(builder.String()))
	req.Header.Set("Authorization", "Bearer fixture-key")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// The gateway base64-encodes the upload before handing it to the worker.
	raw := worker.lastCall().raw
	if !strings.Contains(string(raw), "references") {
		t.Fatalf("references missing from worker payload: %s", raw)
	}
}

func TestCreateRejectsWrongProviderAndModelKind(t *testing.T) {
	worker := &fakeWorker{t: t}
	_, router, _ := newTestHandler(t, worker)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, jsonRequest("/v1/videos", `{"model":"deepseek-v4-flash","prompt":"x"}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported_provider") {
		t.Fatalf("provider gate: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, jsonRequest("/v1/videos", `{"model":"muse-spark","prompt":"x"}`))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported_model") {
		t.Fatalf("model kind gate: %d %s", rec.Code, rec.Body.String())
	}
	if worker.count() != 0 {
		t.Fatalf("worker must not be called: %+v", worker.calls)
	}
}

func TestRetrieveAndContent(t *testing.T) {
	worker := &fakeWorker{t: t}
	_, router, _ := newTestHandler(t, worker)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/videos/vid_1", nil)
	req.Header.Set("Authorization", "Bearer fixture-key")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retrieve: %d %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	if body["status"] != "completed" || body["progress"] != float64(100) {
		t.Fatalf("body=%+v", body)
	}
	if body["has_content"] != true || body["content_url"] != "/v1/videos/vid_1/content" {
		t.Fatalf("content pointer missing: %+v", body)
	}
	if !strings.Contains(worker.lastCall().query, "caller_id=caller") {
		t.Fatalf("ownership query missing: %q", worker.lastCall().query)
	}

	rec = httptest.NewRecorder()
	contentReq := httptest.NewRequest(http.MethodGet, "/v1/videos/vid_1/content", nil)
	contentReq.Header.Set("Authorization", "Bearer fixture-key")
	router.ServeHTTP(rec, contentReq)
	if rec.Code != http.StatusOK || rec.Body.String() != "video-payload" {
		t.Fatalf("content: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "vid_1.mp4") {
		t.Fatalf("disposition=%q", rec.Header().Get("Content-Disposition"))
	}

	rec = httptest.NewRecorder()
	unknownReq := httptest.NewRequest(http.MethodGet, "/v1/videos/vid_missing", nil)
	unknownReq.Header.Set("Authorization", "Bearer fixture-key")
	router.ServeHTTP(rec, unknownReq)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d %s", rec.Code, rec.Body.String())
	}
}

func TestContentRejectsUnfinishedJob(t *testing.T) {
	worker := &fakeWorker{t: t, replies: map[string]reply{
		"GET /v1/videos/vid_1": {status: http.StatusOK, body: map[string]any{"task": map[string]any{
			"id": "vid_1", "status": "processing", "progress": 40, "model": "muse-video",
		}}},
	}}
	_, router, _ := newTestHandler(t, worker)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/videos/vid_1/content", nil)
	req.Header.Set("Authorization", "Bearer fixture-key")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "job_incomplete") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListReturnsOwnJobsOnly(t *testing.T) {
	worker := &fakeWorker{t: t}
	_, router, _ := newTestHandler(t, worker)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/videos", nil)
	req.Header.Set("Authorization", "Bearer fixture-key")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	body := decode(t, rec)
	data, _ := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("data=%+v", body)
	}
	if !strings.Contains(worker.lastCall().query, "caller_id=") {
		t.Fatalf("list must scope by caller: %q", worker.lastCall().query)
	}
}

func TestMediaRoutesRequireACallerKey(t *testing.T) {
	worker := &fakeWorker{t: t}
	_, router, _ := newTestHandler(t, worker)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/videos/vid_1", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRenderTaskErrorSurface(t *testing.T) {
	body := renderTask(mediacompat.VideoTask{
		ID: "vid_1", Status: "failed", Stage: "failed", Model: "muse-video",
		Error: "Muse video job failed", ErrorCode: "browser_error", CreatedAt: 1735689600, UpdatedAt: 1735689700,
	}, "muse-video")
	if body["error"] == nil {
		t.Fatalf("error not rendered: %+v", body)
	}
	if _, ok := body["completed_at"]; ok {
		t.Fatal("a failed job must not report a completion time")
	}
	if body["has_content"] != nil {
		t.Fatal("a failed job has no content")
	}
}

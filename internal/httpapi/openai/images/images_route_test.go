package images

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
	"ds2api/internal/config"
	"ds2api/internal/mediacompat"

	"github.com/go-chi/chi/v5"
)

// capturedCall is what the fake worker saw from the gateway.
type capturedCall struct {
	path    string
	secret  string
	payload map[string]any
}

type fakeWorker struct {
	t        *testing.T
	mu       sync.Mutex
	calls    []capturedCall
	respond  func(path string) (status int, body any)
	response []byte
}

func (f *fakeWorker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("read worker request: %v", err)
		return
	}
	payload := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			f.t.Errorf("worker body is not json: %v (%s)", err, body)
			return
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, capturedCall{path: r.URL.Path, secret: r.Header.Get("X-Worker-Secret"), payload: payload})
	f.mu.Unlock()

	if f.respond != nil {
		if strings.HasPrefix(r.URL.Path, "/v1/media/") && f.response != nil {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(f.response)
			return
		}
		status, body := f.respond(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"model":   "muse-image",
		"created": 1735689600,
		"data": []any{map[string]any{
			"b64_json": f.imageBase64(), "url": nil, "media_id": nil,
			"content_type": "image/png", "width": 8, "height": 8,
		}},
		"usage":      map[string]any{"images": 1},
		"cookies":    "hatch_sess=renewed",
		"thread_url": "https://muse.ai/thread/fake",
	})
}

func (f *fakeWorker) imageBase64() string {
	if len(f.response) > 0 {
		return base64.StdEncoding.EncodeToString(f.response)
	}
	return base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
}

func (f *fakeWorker) lastCall() capturedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return capturedCall{}
	}
	return f.calls[len(f.calls)-1]
}

func newTestHandler(t *testing.T, worker http.Handler) (*Handler, *chi.Mux, *chathistory.Store) {
	t.Setenv("DS2API_MUSE_WORKER_URL", workerURL(t, worker))
	t.Setenv("DS2API_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["fixture-key"],"accounts":[{"name":"muse-fixture","provider":"muse","cookies":"hatch_sess=fixture"}]}`)
	store := config.LoadStore()
	resolver := auth.NewResolver(store, account.NewPool(store), func(context.Context, config.Account) (string, error) {
		t.Fatal("DeepSeek login must not run for muse media")
		return "", nil
	})
	history := chathistory.New(filepath.Join(t.TempDir(), "history.json"))
	handler := &Handler{
		Store: store, Auth: resolver, ChatHistory: history,
		Ownership: NewMediaOwnership(time.Minute),
	}
	router := chi.NewRouter()
	RegisterRoutes(router, handler)
	return handler, router, history
}

func workerURL(t *testing.T, worker http.Handler) string {
	t.Helper()
	server := httptest.NewServer(worker)
	t.Cleanup(server.Close)
	return server.URL
}

func authed(method, url, body string) *http.Request {
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer fixture-key")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return out
}

func firstDataItem(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	switch data := body["data"].(type) {
	case []any:
		if len(data) == 0 {
			t.Fatalf("empty data array: %+v", body)
		}
		item, ok := data[0].(map[string]any)
		if !ok {
			t.Fatalf("data[0] is not an object: %+v", data[0])
		}
		return item
	case []map[string]any:
		if len(data) == 0 {
			t.Fatalf("empty data array: %+v", body)
		}
		return data[0]
	default:
		t.Fatalf("missing data array: %+v", body)
	}
	return nil
}

func TestGenerationsSendsWorkerContractAndReturnsB64(t *testing.T) {
	worker := &fakeWorker{t: t}
	handler, router, history := newTestHandler(t, worker)
	_ = handler

	req := authed("POST", "/v1/images/generations", `{"model":"muse-image","prompt":"a red fox","n":2,"size":"1024x1024","response_format":"b64_json"}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	item := firstDataItem(t, body)
	if got := stringField(item, "b64_json"); got == "" {
		t.Fatalf("b64_json missing: %+v", item)
	}
	if body["model"] != "muse-image" {
		t.Fatalf("model echo=%v", body["model"])
	}
	call := worker.lastCall()
	if call.path != "/v1/images/generations" {
		t.Fatalf("worker path=%s", call.path)
	}
	if call.payload["mode"] != "generation" {
		t.Fatalf("mode=%v", call.payload["mode"])
	}
	if call.payload["prompt"] != "a red fox" || call.payload["n"] != float64(2) {
		t.Fatalf("payload=%+v", call.payload)
	}
	if call.payload["size"] != "1024x1024" {
		t.Fatalf("size=%v", call.payload["size"])
	}
	account, _ := call.payload["account"].(map[string]any)
	if account["cookies"] != "hatch_sess=fixture" {
		t.Fatalf("account payload=%+v", account)
	}
	if stringField(account, "account_id") == "" {
		t.Fatal("worker payload lost the account id")
	}

	snapshot, err := history.Snapshot()
	if err != nil {
		t.Fatalf("history snapshot: %v", err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].Surface != "openai.images_generation" {
		t.Fatalf("history entries=%+v", snapshot.Items)
	}
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}

func TestEditsMultipartUploadsReferenceBytes(t *testing.T) {
	worker := &fakeWorker{t: t}
	handler, router, _ := newTestHandler(t, worker)
	_ = handler

	body, contentType := multipartBody(t, map[string]string{
		"model": "muse-image", "prompt": "make it night", "response_format": "b64_json",
	}, "image", "in.png", pngBytes("reference-bytes"))

	req := httptest.NewRequest("POST", "/v1/images/edits", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer fixture-key")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	call := worker.lastCall()
	if call.path != "/v1/images/edits" {
		t.Fatalf("worker path=%s", call.path)
	}
	if call.payload["mode"] != "edit" {
		t.Fatalf("mode=%v", call.payload["mode"])
	}
	references, _ := call.payload["references"].([]any)
	if len(references) != 1 {
		t.Fatalf("references=%+v", call.payload["references"])
	}
	reference := references[0].(map[string]any)
	if reference["filename"] != "in.png" {
		t.Fatalf("filename=%v", reference["filename"])
	}
	encoded, _ := reference["data_base64"].(string)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(decoded) != string(pngBytes("reference-bytes")) {
		t.Fatalf("reference bytes did not survive the hop: %q", encoded)
	}
}

// pngBytes builds a payload with a real PNG signature so content sniffing,
// validation and rejection paths behave the way they do for genuine uploads.
func pngBytes(payload string) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n"), []byte(payload)...)
}

func TestEditsJSONDataURIReference(t *testing.T) {
	worker := &fakeWorker{t: t}
	handler, router, _ := newTestHandler(t, worker)
	_ = handler

	encoded := base64.StdEncoding.EncodeToString([]byte("reference-bytes"))
	req := authed("POST", "/v1/images/edits", `{"model":"muse-image","prompt":"brighter sky","image":"data:image/png;base64,`+encoded+`"}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	call := worker.lastCall()
	references, _ := call.payload["references"].([]any)
	if len(references) != 1 {
		t.Fatalf("references=%+v", call.payload["references"])
	}
}

func TestInvalidRequestsFailBeforeTheWorker(t *testing.T) {
	t.Run("unsupported provider", func(t *testing.T) {
		local := &fakeWorker{t: t}
		_, fakeRouter, _ := newTestHandler(t, local)
		rec := httptest.NewRecorder()
		fakeRouter.ServeHTTP(rec, authed("POST", "/v1/images/generations", `{"model":"deepseek-v4-flash","prompt":"cat"}`))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported_provider") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if len(local.calls) != 0 {
			t.Fatalf("worker must not be called: %+v", local.calls)
		}
	})

	t.Run("bad n", func(t *testing.T) {
		local := &fakeWorker{t: t}
		_, fakeRouter, _ := newTestHandler(t, local)
		rec := httptest.NewRecorder()
		fakeRouter.ServeHTTP(rec, authed("POST", "/v1/images/generations", `{"model":"muse-image","prompt":"cat","n":9}`))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "n must be between") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if len(local.calls) != 0 {
			t.Fatal("worker must not be called")
		}
	})

	t.Run("missing prompt", func(t *testing.T) {
		local := &fakeWorker{t: t}
		_, fakeRouter, _ := newTestHandler(t, local)
		rec := httptest.NewRecorder()
		fakeRouter.ServeHTTP(rec, authed("POST", "/v1/images/generations", `{"model":"muse-image"}`))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("chat model on image transport", func(t *testing.T) {
		local := &fakeWorker{t: t}
		_, fakeRouter, _ := newTestHandler(t, local)
		rec := httptest.NewRecorder()
		fakeRouter.ServeHTTP(rec, authed("POST", "/v1/images/generations", `{"model":"muse-spark","prompt":"cat"}`))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported_model") {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		local := &fakeWorker{t: t}
		_, fakeRouter, _ := newTestHandler(t, local)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"muse-image","prompt":"cat"}`))
		req.Header.Set("Content-Type", "application/json")
		fakeRouter.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestThrottleFromWorkerMapsTo429(t *testing.T) {
	worker := &fakeWorker{t: t, respond: func(string) (int, any) {
		return http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded on muse account", "code": "rate_limit_exceeded", "status": 429}
	}}
	handler, router, _ := newTestHandler(t, worker)
	_ = handler

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authed("POST", "/v1/images/generations", `{"model":"muse-image","prompt":"cat"}`))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestURLModeRegistersOwnershipForMediaBytes(t *testing.T) {
	worker := &fakeWorker{t: t, response: []byte("fake-png-bytes"), respond: func(string) (int, any) {
		return http.StatusOK, map[string]any{
			"status": "ok", "model": "muse-image", "created": 1735689600,
			"data": []any{map[string]any{
				"b64_json": nil, "url": "http://worker.internal/v1/media/med_alpha", "media_id": "med_alpha",
				"content_type": "image/png", "width": 8, "height": 8,
			}},
		}
	}}
	handler, router, _ := newTestHandler(t, worker)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, authed("POST", "/v1/images/generations", `{"model":"muse-image","prompt":"cat","response_format":"url"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	item := firstDataItem(t, decodeBody(t, rec))
	url := stringField(item, "url")
	if url != "/v1/images/media/med_alpha" && !strings.HasSuffix(url, "/v1/images/media/med_alpha") {
		t.Fatalf("url should point at the gateway media route, got %q", url)
	}

	fetch := httptest.NewRequest("GET", "/v1/images/media/med_alpha", nil)
	fetch.Header.Set("Authorization", "Bearer fixture-key")
	fetchRec := httptest.NewRecorder()
	router.ServeHTTP(fetchRec, fetch)
	if fetchRec.Code != http.StatusOK || fetchRec.Body.String() != "fake-png-bytes" {
		t.Fatalf("media fetch: %d %q", fetchRec.Code, fetchRec.Body.String())
	}

	if _, ok := handler.Ownership.claim("caller:nobody", "med_alpha"); ok {
		t.Fatal("a different caller must not claim the media id")
	}
}

func TestMediaBytesRejectsUnknownID(t *testing.T) {
	worker := &fakeWorker{t: t}
	handler, router, _ := newTestHandler(t, worker)
	handler.Ownership = NewMediaOwnership(time.Minute)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/images/media/med_missing", nil)
	req.Header.Set("Authorization", "Bearer fixture-key")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func multipartBody(t *testing.T, fields map[string]string, fileField, fileName string, fileBytes []byte) (string, string) {
	t.Helper()
	var buf strings.Builder
	writer := multipart.NewWriter(&nopWriterCloser{&buf})
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(fileBytes); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return buf.String(), writer.FormDataContentType()
}

type nopWriterCloser struct {
	builder *strings.Builder
}

func (n *nopWriterCloser) Write(p []byte) (int, error) { return n.builder.Write(p) }
func (n *nopWriterCloser) Close() error                { return nil }

func TestRenderKeepsB64WhenWorkerGivesNeitherURLNorID(t *testing.T) {
	request := mediacompat.ImageRequest{
		Surface: mediacompat.SurfaceImagesGeneration, ResponseModel: "muse-image", Prompt: "cat",
	}
	result := mediacompat.MediaResult{
		Model: "muse-image", Created: 1735689600,
		Items: []mediacompat.MediaItem{{ContentType: "image/png", Width: 8, Height: 8}},
	}
	body := renderImagesResponse(request, result, "https://gateway.example")
	item := firstDataItem(t, body)
	if _, ok := item["b64_json"]; !ok {
		t.Fatalf("expected an explicit empty b64_json slot: %+v", item)
	}
	if _, ok := item["url"]; ok {
		t.Fatalf("no url should be invented: %+v", item)
	}
}

func TestAbsoluteMediaURLPrefersGatewayRoute(t *testing.T) {
	withID := mediacompat.MediaItem{MediaID: "med_1", URL: "https://elsewhere.test/x.png"}
	if got := absoluteMediaURL("https://gateway.example/", withID); got != "https://gateway.example/v1/images/media/med_1" {
		t.Fatalf("gateway route expected, got %q", got)
	}
	withoutID := mediacompat.MediaItem{URL: "https://cdn.muse.ai/x.png"}
	if got := absoluteMediaURL("https://gateway.example", withoutID); got != "https://cdn.muse.ai/x.png" {
		t.Fatalf("absolute upstream url expected, got %q", got)
	}
}

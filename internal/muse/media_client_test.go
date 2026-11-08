package muse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ds2api/internal/account"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/mediacompat"
)

type mediaWorker struct {
	t     *testing.T
	mu    sync.Mutex
	URL   string
	calls []mediaCall
	// respondByAccount lets a test fail one account and pass another.
	respondByAccount map[string]mediaReply
	reply            mediaReply
}

type mediaCall struct {
	path    string
	secret  string
	payload map[string]any
	query   string
}

type mediaReply struct {
	status int
	body   any
}

func (w *mediaWorker) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		w.t.Errorf("read body: %v", err)
		return
	}
	payload := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			w.t.Errorf("body is not json: %v", err)
			return
		}
	}
	w.mu.Lock()
	w.calls = append(w.calls, mediaCall{path: req.URL.Path, secret: req.Header.Get("X-Worker-Secret"), payload: payload, query: req.URL.RawQuery})
	reply := w.reply
	if accountID, _ := accountIDOf(payload); accountID != "" {
		if override, ok := w.respondByAccount[accountID]; ok {
			reply = override
		}
	}
	w.mu.Unlock()

	if reply.body == nil {
		reply = mediaReply{status: http.StatusOK, body: defaultImageReply()}
	}
	rw.Header().Set("Content-Type", "application/json")
	if strings.Contains(req.URL.Path, "/content") || strings.Contains(req.URL.Path, "/v1/media/") {
		rw.Header().Set("Content-Type", "image/png")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("media-bytes"))
		return
	}
	rw.WriteHeader(reply.status)
	_ = json.NewEncoder(rw).Encode(reply.body)
}

func accountIDOf(payload map[string]any) (string, bool) {
	account, ok := payload["account"].(map[string]any)
	if !ok {
		return "", false
	}
	id, _ := account["account_id"].(string)
	return id, id != ""
}

func defaultImageReply() map[string]any {
	return map[string]any{
		"status": "ok", "model": "muse-image", "created": 1735689600,
		"data": []any{map[string]any{
			"b64_json":     base64.StdEncoding.EncodeToString([]byte("png")),
			"content_type": "image/png", "width": 8, "height": 8,
		}},
		"usage":      map[string]any{"images": 1},
		"cookies":    "hatch_sess=renewed",
		"thread_url": "https://muse.ai/thread/1",
	}
}

func (w *mediaWorker) last() mediaCall {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.calls) == 0 {
		return mediaCall{}
	}
	return w.calls[len(w.calls)-1]
}

func (w *mediaWorker) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.calls)
}

func newMediaClient(t *testing.T, worker *mediaWorker, accountName, cookies string) (*Client, *string) {
	t.Helper()
	client := NewClient(worker.URL, "worker-secret", "http://proxy.test:8080", config.Account{
		Name: accountName, Provider: "muse", Cookies: cookies,
	})
	var updated string
	client.OnCookieUpdate = func(cookies string) { updated = cookies }
	t.Cleanup(func() { DefaultRuntime().EvictClient(accountName) })
	return client, &updated
}

func imageRequest(mode string) mediacompat.ImageRequest {
	req := mediacompat.ImageRequest{
		Surface: mediacompat.SurfaceImagesGeneration, RequestedModel: "muse-image",
		ResolvedModel: "muse-image", ResponseModel: "muse-image", Prompt: "a red fox",
		N: 2, Size: "1024x1024", ResponseFormat: mediacompat.ResponseFormatB64JSON, CallerID: "caller:me",
	}
	if mode == "edit" {
		req.Surface = mediacompat.SurfaceImagesEdit
		req.References = []mediacompat.Reference{{Filename: "in.png", ContentType: "image/png", Data: []byte("bytes")}}
	}
	return req
}

func TestExecuteImageSendsWorkerContract(t *testing.T) {
	worker := startMediaWorker(t, nil)
	client, updated := newMediaClient(t, worker, "muse-one", "hatch_sess=fixture")

	result, err := client.ExecuteImage(context.Background(), imageRequest("generation"))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	call := worker.last()
	if call.path != "/v1/images/generations" {
		t.Fatalf("path=%s", call.path)
	}
	if call.secret != "worker-secret" {
		t.Fatalf("worker secret header missing: %q", call.secret)
	}
	if call.payload["mode"] != "generation" || call.payload["n"] != float64(2) || call.payload["size"] != "1024x1024" {
		t.Fatalf("payload=%+v", call.payload)
	}
	if call.payload["caller_id"] != "caller:me" {
		t.Fatalf("caller id not forwarded: %+v", call.payload)
	}
	accountPayload, _ := call.payload["account"].(map[string]any)
	if accountPayload["cookies"] != "hatch_sess=fixture" || accountPayload["proxy"] != "http://proxy.test:8080" {
		t.Fatalf("account payload=%+v", accountPayload)
	}
	if len(result.Items) != 1 || result.Items[0].B64JSON == "" {
		t.Fatalf("result=%+v", result)
	}
	if result.Created != 1735689600 || result.ThreadURL != "https://muse.ai/thread/1" {
		t.Fatalf("metadata lost: %+v", result)
	}
	if *updated != "hatch_sess=renewed" {
		t.Fatalf("cookie write-back did not run: %q", *updated)
	}
}

func TestExecuteImageUsesEditRouteAndReferences(t *testing.T) {
	worker := startMediaWorker(t, nil)
	client, _ := newMediaClient(t, worker, "muse-two", "hatch_sess=fixture")

	if _, err := client.ExecuteImage(context.Background(), imageRequest("edit")); err != nil {
		t.Fatalf("execute edit: %v", err)
	}
	call := worker.last()
	if call.path != "/v1/images/edits" || call.payload["mode"] != "edit" {
		t.Fatalf("edit route: %s %+v", call.path, call.payload)
	}
	references, _ := call.payload["references"].([]any)
	if len(references) != 1 {
		t.Fatalf("references=%+v", call.payload["references"])
	}
	reference := references[0].(map[string]any)
	decoded, err := base64.StdEncoding.DecodeString(reference["data_base64"].(string))
	if err != nil || string(decoded) != "bytes" {
		t.Fatalf("reference bytes lost: %v %q", err, reference["data_base64"])
	}
}

func TestExecuteImageRejectsChatModel(t *testing.T) {
	worker := startMediaWorker(t, nil)
	client, _ := newMediaClient(t, worker, "muse-three", "hatch_sess=fixture")

	request := imageRequest("generation")
	request.ResolvedModel = "muse-spark"
	_, err := client.ExecuteImage(context.Background(), request)
	if !errors.Is(err, ErrMuseInvalidModel) {
		t.Fatalf("expected invalid model, got %v", err)
	}
	if worker.count() != 0 {
		t.Fatal("worker must not be called for a wrong model kind")
	}
}

func TestExecuteImageErrorMapping(t *testing.T) {
	cases := []struct {
		name        string
		reply       mediaReply
		wantErr     error
		wantMessage string
	}{
		{"throttle", mediaReply{status: http.StatusTooManyRequests, body: map[string]any{"error": "rate limit exceeded", "code": "rate_limit_exceeded"}}, ErrMuseThrottled, "rate limit"},
		{"auth", mediaReply{status: http.StatusUnauthorized, body: map[string]any{"error": "session expired", "code": "session_expired"}}, ErrMuseUnauthorized, "session_expired"},
		{"upload unconfirmed", mediaReply{status: http.StatusBadGateway, body: map[string]any{"error": "not confirmed", "code": "upload_unconfirmed"}}, nil, "upload_unconfirmed"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			worker := startMediaWorker(t, &testCase.reply)
			client, _ := newMediaClient(t, worker, "muse-"+testCase.name, "hatch_sess=fixture")
			_, err := client.ExecuteImage(context.Background(), imageRequest("generation"))
			if err == nil {
				t.Fatal("expected error")
			}
			if testCase.wantErr != nil && !errors.Is(err, testCase.wantErr) {
				t.Fatalf("expected %v, got %v", testCase.wantErr, err)
			}
			if !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Fatalf("message %q lost %q", err.Error(), testCase.wantMessage)
			}
			status, code := MediaErrorDetail(err)
			if status == http.StatusOK || code == "" {
				t.Fatalf("detail mapping: %d %q", status, code)
			}
		})
	}
}

func TestExecuteImageRejectsEmptyMedia(t *testing.T) {
	reply := mediaReply{status: http.StatusOK, body: map[string]any{"status": "ok", "data": []any{}}}
	worker := startMediaWorker(t, &reply)
	client, _ := newMediaClient(t, worker, "muse-empty", "hatch_sess=fixture")

	if _, err := client.ExecuteImage(context.Background(), imageRequest("generation")); err == nil {
		t.Fatal("an empty media array must fail instead of returning a blank result")
	}
}

func TestVideoClientContracts(t *testing.T) {
	worker := startMediaWorker(t, nil)
	client, _ := newMediaClient(t, worker, "muse-video-account", "hatch_sess=fixture")
	worker.reply = mediaReply{status: http.StatusCreated, body: map[string]any{
		"status": "ok",
		"task":   map[string]any{"id": "vid_1", "status": "queued", "stage": "queued", "model": "muse-video", "created_at": 1.0},
	}}

	task, err := client.CreateVideo(context.Background(), mediacompat.VideoRequest{
		RequestedModel: "muse-video", ResolvedModel: "muse-video", ResponseModel: "muse-video",
		Prompt: "a slow orbit", CallerID: "caller:me", DurationSeconds: 6,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.ID != "vid_1" || task.Status != "queued" || task.Model != "muse-video" {
		t.Fatalf("task=%+v", task)
	}
	call := worker.last()
	if call.path != "/v1/videos" || call.payload["caller_id"] != "caller:me" || call.payload["duration"] != float64(6) {
		t.Fatalf("create call: %s %+v", call.path, call.payload)
	}

	worker.reply = mediaReply{status: http.StatusOK, body: map[string]any{
		"task": map[string]any{"id": "vid_1", "status": "completed", "progress": 100, "media_id": "med_9", "content_type": "video/mp4", "size_bytes": 11},
	}}
	polled, err := client.PollVideo(context.Background(), "vid_1", "caller:me")
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if !polled.IsTerminal() || polled.MediaID != "med_9" || polled.SizeBytes != 11 {
		t.Fatalf("polled=%+v", polled)
	}
	if !strings.Contains(worker.last().query, "caller_id=caller%3Ame") {
		t.Fatalf("caller id must be forwarded for ownership: %q", worker.last().query)
	}

	data, contentType, err := client.FetchVideoContent(context.Background(), "vid_1", "caller:me")
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	if string(data) != "media-bytes" || contentType != "image/png" {
		t.Fatalf("content=%q type=%q", data, contentType)
	}

	worker.reply = mediaReply{status: http.StatusOK, body: map[string]any{
		"data": []any{map[string]any{"id": "vid_1", "status": "completed"}, map[string]any{"id": "vid_2", "status": "failed", "error_code": "browser_error"}},
	}}
	tasks, err := client.ListVideos(context.Background(), "caller:me")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 2 || tasks[1].ErrorCode != "browser_error" {
		t.Fatalf("tasks=%+v", tasks)
	}

	blob, blobType, err := client.FetchMediaBytes(context.Background(), "med_9")
	if err != nil || string(blob) != "media-bytes" || blobType == "" {
		t.Fatalf("media bytes: %q %q %v", blob, blobType, err)
	}
}

func TestPollVideoForeignCallerIsNotFound(t *testing.T) {
	reply := mediaReply{status: http.StatusNotFound, body: map[string]any{"error": "video job not found", "code": "not_found"}}
	worker := startMediaWorker(t, &reply)
	client, _ := newMediaClient(t, worker, "muse-other", "hatch_sess=fixture")

	_, err := client.PollVideo(context.Background(), "vid_x", "caller:not-the-owner")
	if !errors.Is(err, ErrMuseJobNotFound) {
		t.Fatalf("expected ErrMuseJobNotFound, got %v", err)
	}
	status, code := MediaErrorDetail(err)
	if status != http.StatusNotFound || code != "not_found" {
		t.Fatalf("detail=%d %q", status, code)
	}
}

func TestCreateVideoRejectsChatModel(t *testing.T) {
	worker := startMediaWorker(t, nil)
	client, _ := newMediaClient(t, worker, "muse-kind", "hatch_sess=fixture")

	_, err := client.CreateVideo(context.Background(), mediacompat.VideoRequest{
		ResolvedModel: "muse-spark", RequestedModel: "muse-spark", Prompt: "x",
	})
	if !errors.Is(err, ErrMuseInvalidModel) {
		t.Fatalf("expected invalid model, got %v", err)
	}
}

func TestMediaErrorDetailMatrix(t *testing.T) {
	cases := map[error]struct {
		status int
		code   string
	}{
		ErrMuseInvalidModel:           {http.StatusBadRequest, "unsupported_model"},
		ErrMuseJobNotFound:            {http.StatusNotFound, "not_found"},
		ErrAllMuseAccountsCoolingDown: {http.StatusTooManyRequests, "all_muse_accounts_cooling_down"},
		ErrMuseThrottled:              {http.StatusTooManyRequests, "rate_limit_exceeded"},
		ErrMuseUnauthorized:           {http.StatusUnauthorized, "session_expired"},
		ErrMuseWorkerUnavailable:      {http.StatusBadGateway, "worker_unavailable"},
	}
	for err, want := range cases {
		status, code := MediaErrorDetail(err)
		if status != want.status || code != want.code {
			t.Fatalf("%v mapped to %d %q, want %d %q", err, status, code, want.status, want.code)
		}
	}
	if status, _ := MediaErrorDetail(nil); status != http.StatusOK {
		t.Fatal("nil error must map to 200")
	}
}

func TestExecuteImageWithRetryRotatesOnThrottle(t *testing.T) {
	t.Setenv("DS2API_CONFIG_PATH", filepath.Join(t.TempDir(), "config.json"))
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["fixture-key"],"accounts":[
		{"name":"muse-a","provider":"muse","cookies":"hatch_sess=a"},
		{"name":"muse-b","provider":"muse","cookies":"hatch_sess=b"}]}`)
	store := config.LoadStore()

	throttled := mediaReply{status: http.StatusTooManyRequests, body: map[string]any{"error": "rate limit exceeded", "code": "rate_limit_exceeded"}}
	worker := &mediaWorker{t: t, reply: mediaReply{status: http.StatusOK, body: defaultImageReply()},
		respondByAccount: map[string]mediaReply{"muse-a": throttled}}
	server := httptest.NewServer(worker)
	t.Cleanup(server.Close)
	t.Setenv("DS2API_MUSE_WORKER_URL", server.URL)
	t.Cleanup(func() { DefaultRuntime().EvictClient("muse-a"); DefaultRuntime().EvictClient("muse-b") })

	resolver := auth.NewResolver(store, account.NewPool(store), func(context.Context, config.Account) (string, error) {
		t.Fatal("DeepSeek login must not run for muse media")
		return "", nil
	})
	req, err := http.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer fixture-key")
	a, err := resolver.DetermineForProvider(req, "muse")
	if err != nil {
		t.Fatalf("auth: %v", err)
	}

	request := imageRequest("generation")
	result, err := ExecuteImageWithRetry(context.Background(), a, store, request, func(string) {})
	if err != nil {
		t.Fatalf("expected rotation to recover, got %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("empty result after rotation")
	}
	if worker.count() < 2 {
		t.Fatalf("expected a retry on another account, calls=%d", worker.count())
	}
}

func startMediaWorker(t *testing.T, reply *mediaReply) *mediaWorker {
	t.Helper()
	worker := &mediaWorker{t: t}
	if reply != nil {
		worker.reply = *reply
	}
	server := httptest.NewServer(worker)
	t.Cleanup(server.Close)
	worker.URL = server.URL
	return worker
}

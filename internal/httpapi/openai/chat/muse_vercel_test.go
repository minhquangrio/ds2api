package chat

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMuseAliasPrepareAvoidsAuthAndUploads(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "fixture-secret")
	h := &Handler{Store: mockOpenAIConfig{aliases: map[string]string{"custom-alias": "muse-spark"}}}
	// Auth and DS are deliberately nil: routing must finish before either is used.
	r := httptest.NewRequest("POST", "/v1/chat/completions?__stream_prepare=1", strings.NewReader(`{"model":"custom-alias","stream":true}`))
	r.Header.Set("X-Ds2-Internal-Token", "fixture-secret")
	w := httptest.NewRecorder()
	h.handleVercelStreamPrepare(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"route":"proxy_to_go"`) {
		t.Fatalf("incorrect alias route: %d %s", w.Code, w.Body.String())
	}
}

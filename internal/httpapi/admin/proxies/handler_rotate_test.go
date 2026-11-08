package proxies

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	"ds2api/internal/proxyrotate"
)

func newRotatingProviderStub(t *testing.T, body string) *string {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, `{"status":101,"comen":"key does not exist"}`)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(stub.Close)
	url := stub.URL
	return &url
}

func TestAddRotatingProxyPersistsRotateBlock(t *testing.T) {
	endpoint := newRotatingProviderStub(t, `{"status":100,"message":"proxy nay se die sau 600s","proxyhttp":"10.0.0.1:1080::"}`)
	h := newAdminProxyTestHandler(t, `{"accounts":[]}`)

	r := chi.NewRouter()
	r.Post("/admin/proxies", h.addProxy)

	req := httptest.NewRequest(http.MethodPost, "/admin/proxies", bytes.NewBufferString(fmt.Sprintf(`{
		"name":"Xoay VN",
		"rotate":{"provider":"custom","endpoint":%q,"key":"secret-key","network":"fpt","province":"0","protocol":"http","whitelist":"1.2.3.4"}
	}`, *endpoint)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	proxies := h.Store.Snapshot().Proxies
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %#v", proxies)
	}
	proxy := config.NormalizeProxy(proxies[0])
	if !proxy.IsRotating() {
		t.Fatalf("expected rotate block, got %#v", proxy)
	}
	if proxy.Type != "http" || proxy.Host != "" || proxy.Port != 0 {
		t.Fatalf("rotating entry must not keep static fields: %#v", proxy)
	}
	if proxy.Rotate.Key != "secret-key" || proxy.Rotate.Network != "fpt" || proxy.Rotate.Whitelist != "1.2.3.4" {
		t.Fatalf("unexpected rotate config: %#v", proxy.Rotate)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertNoSecretInResponse(t, payload)
}

func assertNoSecretInResponse(t *testing.T, payload map[string]any) {
	t.Helper()
	proxy, _ := payload["proxy"].(map[string]any)
	if proxy == nil {
		t.Fatalf("missing proxy in payload: %#v", payload)
	}
	if _, exists := proxy["password"]; exists {
		t.Fatalf("password must not be returned: %#v", proxy)
	}
	if mode, _ := proxy["mode"].(string); mode != "rotate" {
		t.Fatalf("expected rotate mode, got %#v", proxy)
	}
	rotate, _ := proxy["rotate"].(map[string]any)
	if rotate == nil {
		t.Fatalf("expected rotate view: %#v", proxy)
	}
	if key, exists := rotate["key"]; exists {
		t.Fatalf("rotate key must not be returned: %#v", key)
	}
	if hasKey, _ := rotate["has_key"].(bool); !hasKey {
		t.Fatalf("expected has_key=true, got %#v", rotate)
	}
	preview, _ := rotate["key_preview"].(string)
	if preview == "" || preview == "secret-key" {
		t.Fatalf("expected masked key preview, got %q", preview)
	}
}

func TestUpdateRotatingProxyKeepsKeyWhenOmitted(t *testing.T) {
	endpoint := newRotatingProviderStub(t, `{"status":100,"message":"ok","proxyhttp":"10.0.0.1:1080::"}`)
	h := newAdminProxyTestHandler(t, fmt.Sprintf(`{
		"proxies":[{"id":"px","name":"Xoay","type":"http","rotate":{"provider":"custom","endpoint":%q,"key":"keep-me","protocol":"http"}}]
	}`, *endpoint))

	r := chi.NewRouter()
	r.Put("/admin/proxies/{proxyID}", h.updateProxy)

	req := httptest.NewRequest(http.MethodPut, "/admin/proxies/px", bytes.NewBufferString(`{"name":"Renamed","rotate":{"provider":"custom","endpoint":"","protocol":"http"}}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	proxy := config.NormalizeProxy(h.Store.Snapshot().Proxies[0])
	if proxy.Rotate == nil || proxy.Rotate.Key != "keep-me" {
		t.Fatalf("expected stored key to be preserved, got %#v", proxy.Rotate)
	}
	if proxy.Name != "Renamed" {
		t.Fatalf("expected renamed proxy, got %#v", proxy)
	}
}

func TestUpdateProxyClearsRotationOnlyWhenRequested(t *testing.T) {
	endpoint := newRotatingProviderStub(t, `{"status":100,"message":"ok","proxyhttp":"10.0.0.1:1080::"}`)
	raw := fmt.Sprintf(`{
		"proxies":[{"id":"px","name":"Xoay","type":"http","rotate":{"provider":"custom","endpoint":%q,"key":"k1","protocol":"http"}}]
	}`, *endpoint)

	t.Run("no rotate field keeps rotation", func(t *testing.T) {
		h := newAdminProxyTestHandler(t, raw)
		r := chi.NewRouter()
		r.Put("/admin/proxies/{proxyID}", h.updateProxy)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/admin/proxies/px", bytes.NewBufferString(`{"name":"still rotating"}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		if !config.NormalizeProxy(h.Store.Snapshot().Proxies[0]).IsRotating() {
			t.Fatal("expected rotation to survive a partial update")
		}
	})

	t.Run("explicit null switches back to static", func(t *testing.T) {
		h := newAdminProxyTestHandler(t, raw)
		r := chi.NewRouter()
		r.Put("/admin/proxies/{proxyID}", h.updateProxy)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/admin/proxies/px", bytes.NewBufferString(`{"name":"Static","rotate":null,"type":"socks5h","host":"127.0.0.1","port":1080}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		proxy := config.NormalizeProxy(h.Store.Snapshot().Proxies[0])
		if proxy.IsRotating() {
			t.Fatalf("expected rotation cleared, got %#v", proxy)
		}
		if proxy.Host != "127.0.0.1" || proxy.Port != 1080 {
			t.Fatalf("expected static fields restored, got %#v", proxy)
		}
	})
}

func TestRefreshRotatingProxyFetchesNewNode(t *testing.T) {
	endpoint := newRotatingProviderStub(t, `{"status":100,"message":"proxy nay se die sau 900s","proxyhttp":"10.9.9.9:1080::"}`)
	h := newAdminProxyTestHandler(t, fmt.Sprintf(`{
		"proxies":[{"id":"px","name":"Xoay","type":"http","rotate":{"provider":"custom","endpoint":%q,"key":"k1","ttl_seconds":900}}]
	}`, *endpoint))
	defer proxyrotate.Global().Forget("px")

	r := chi.NewRouter()
	r.Route("/admin", func(admin chi.Router) {
		admin.Route("/proxies", func(sub chi.Router) {
			sub.Post("/{proxyID}/refresh", h.refreshProxy)
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/proxies/px/refresh", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["endpoint"] != "10.9.9.9" || payload["port"] != float64(1080) {
		t.Fatalf("unexpected refresh payload: %#v", payload)
	}

	listRec := httptest.NewRecorder()
	lister := chi.NewRouter()
	lister.Get("/admin/proxies", h.listProxies)
	lister.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/admin/proxies", nil))
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	rotate := listed.Items[0]["rotate"].(map[string]any)
	lease, ok := rotate["lease"].(map[string]any)
	if !ok {
		t.Fatalf("expected live lease in the list view: %#v", rotate)
	}
	if lease["host"] != "10.9.9.9" {
		t.Fatalf("unexpected lease: %#v", lease)
	}
}

func TestRefreshStaticProxyIsRejected(t *testing.T) {
	h := newAdminProxyTestHandler(t, `{"proxies":[{"id":"px","type":"socks5h","host":"127.0.0.1","port":1080}]}`)
	r := chi.NewRouter()
	r.Post("/admin/proxies/{proxyID}/refresh", h.refreshProxy)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/proxies/px/refresh", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTestProxyPassesRotatingEntryToConnectivityTester(t *testing.T) {
	endpoint := newRotatingProviderStub(t, `{"status":100,"message":"ok","proxyhttp":"10.0.0.1:1080::"}`)
	h := newAdminProxyTestHandler(t, fmt.Sprintf(`{
		"proxies":[{"id":"px","name":"Xoay","type":"http","rotate":{"provider":"custom","endpoint":%q,"key":"k1"}}]
	}`, *endpoint))
	defer proxyrotate.Global().Forget("px")

	original := proxyConnectivityTester
	defer func() { proxyConnectivityTester = original }()
	var got config.Proxy
	proxyConnectivityTester = func(_ context.Context, proxy config.Proxy) map[string]any {
		got = proxy
		return map[string]any{"success": true, "proxy_id": proxy.ID}
	}

	r := chi.NewRouter()
	r.Post("/admin/proxies/test", h.testProxy)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/proxies/test", bytes.NewBufferString(`{"proxy_id":"px"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !got.IsRotating() {
		t.Fatalf("the tester must receive the configured rotating entry, got %#v", got)
	}
}

func TestAddRotatingProxyRequiresKey(t *testing.T) {
	endpoint := newRotatingProviderStub(t, `{"status":100,"message":"ok","proxyhttp":"10.0.0.1:1080::"}`)
	h := newAdminProxyTestHandler(t, `{"accounts":[]}`)
	r := chi.NewRouter()
	r.Post("/admin/proxies", h.addProxy)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/proxies", bytes.NewBufferString(fmt.Sprintf(`{"rotate":{"provider":"custom","endpoint":%q}}`, *endpoint))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing rotate key, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// The WebUI proxy tab reads /admin/config, so the redacted view has to appear
// there too, including the live lease and without the key.
func TestConfigEndpointExposesRotateViewWithoutKey(t *testing.T) {
	endpoint := newRotatingProviderStub(t, `{"status":100,"message":"proxy nay se die sau 600s","proxyhttp":"10.5.5.5:1080::"}`)
	router := newHTTPAdminHarness(t, fmt.Sprintf(`{
		"keys":["k1"],
		"proxies":[{"id":"px","name":"Xoay VN","type":"http","rotate":{"provider":"custom","endpoint":%q,"key":"super-secret","protocol":"http"}}]
	}`, *endpoint), &testingDSMock{})
	defer proxyrotate.Global().Forget("px")

	// Populate a lease through the refresh endpoint so the view has something
	// to report.
	postRec := httptest.NewRecorder()
	router.ServeHTTP(postRec, adminReq(http.MethodPost, "/proxies/px/refresh", nil))
	if postRec.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", postRec.Code, postRec.Body.String())
	}

	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, adminReq(http.MethodGet, "/config", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	if strings.Contains(getRec.Body.String(), "super-secret") {
		t.Fatal("the rotating key must never be returned by /admin/config")
	}
	var payload struct {
		Proxies []map[string]any `json:"proxies"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Proxies) != 1 {
		t.Fatalf("unexpected proxies: %#v", payload.Proxies)
	}
	proxy := payload.Proxies[0]
	if proxy["mode"] != "rotate" {
		t.Fatalf("expected rotate mode, got %#v", proxy)
	}
	rotate, _ := proxy["rotate"].(map[string]any)
	if rotate == nil || rotate["has_key"] != true {
		t.Fatalf("expected rotate view with a key marker, got %#v", proxy)
	}
	if preview, _ := rotate["key_preview"].(string); preview == "" || preview == "super-secret" {
		t.Fatalf("expected a masked preview, got %q", preview)
	}
}

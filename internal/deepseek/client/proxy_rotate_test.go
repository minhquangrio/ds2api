package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ds2api/internal/config"
	"ds2api/internal/proxyrotate"
)

type stubDoer struct {
	body string
	err  error
}

func (s stubDoer) Do(*http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(s.body))}, nil
}

func proxyCacheHost(t *testing.T, client *Client) []string {
	t.Helper()
	client.proxyClientsMu.RLock()
	defer client.proxyClientsMu.RUnlock()
	keys := make([]string, 0, len(client.proxyClients))
	for key := range client.proxyClients {
		keys = append(keys, key)
	}
	return keys
}

func TestTrackedDoerCountsInFlightRequests(t *testing.T) {
	usage := &proxyUsage{}
	doer := newTrackedDoer(stubDoer{body: "ok"}, usage)

	resp, err := doer.Do(httptest.NewRequest(http.MethodGet, "http://example.test", nil))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got := usage.inFlight(); got != 1 {
		t.Fatalf("in flight = %d, want 1 while the body is open", got)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if got := usage.inFlight(); got != 0 {
		t.Fatalf("in flight = %d, want 0 after Close", got)
	}
	// A double close must not decrement the counter twice.
	_ = resp.Body.Close()
	if got := usage.inFlight(); got != 0 {
		t.Fatalf("in flight = %d after a second Close", got)
	}
}

func TestTrackedDoerReleasesOnError(t *testing.T) {
	usage := &proxyUsage{}
	doer := newTrackedDoer(stubDoer{err: errors.New("boom")}, usage)
	if _, err := doer.Do(httptest.NewRequest(http.MethodGet, "http://example.test", nil)); err == nil {
		t.Fatal("expected the inner error to surface")
	}
	if got := usage.inFlight(); got != 0 {
		t.Fatalf("in flight = %d, want 0 after a failed request", got)
	}
}

func newRotatingStoreProxy(t *testing.T, endpoint string) (*config.Store, config.Account) {
	t.Helper()
	raw := fmt.Sprintf(`{
		"proxies":[{"id":"px","name":"Xoay","rotate":{"provider":"custom","endpoint":%q,"key":"k1","ttl_seconds":600}}],
		"accounts":[{"email":"u@example.com","password":"pwd","proxy_id":"px"}]
	}`, endpoint)
	t.Setenv("DS2API_CONFIG_JSON", raw)
	t.Setenv("DS2API_ENV_WRITEBACK", "0")
	store := config.LoadStore()
	// Leases live in the process-wide manager, so each test must leave no trace.
	t.Cleanup(func() { proxyrotate.Global().Forget("px") })
	acc, ok := store.FindAccount("u@example.com")
	if !ok {
		t.Fatal("expected account")
	}
	return store, acc
}

func TestRequestClientsForRotatingAccountUsesFetchedNode(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":100,"message":"proxy nay se die sau 600s","proxyhttp":"10.20.30.40:10836::"}`)
	}))
	t.Cleanup(provider.Close)

	store, acc := newRotatingStoreProxy(t, provider.URL)
	client := NewClient(store, nil)
	defer client.Close()

	clients := client.requestClientsForAccount(context.Background(), acc)
	if clients.regular == nil {
		t.Fatal("expected a bundle for the rotating account")
	}

	keys := proxyCacheHost(t, client)
	if len(keys) != 1 || !strings.Contains(keys[0], "10.20.30.40") || !strings.Contains(keys[0], "10836") {
		t.Fatalf("expected the bundle to be keyed by the fetched node, got %#v", keys)
	}
}

func TestRequestClientsForRotatingAccountDoesNotGoDirect(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":102,"message":"key khong ton tai"}`)
	}))
	t.Cleanup(provider.Close)

	store, acc := newRotatingStoreProxy(t, provider.URL)
	client := NewClient(store, nil)
	defer client.Close()

	clients := client.requestClientsForAccount(context.Background(), acc)
	_, err := clients.regular.Do(httptest.NewRequest(http.MethodGet, "https://chat.deepseek.com/api/v3/any", nil))
	if err == nil {
		t.Fatal("expected the request to fail instead of connecting directly")
	}
	if !strings.Contains(err.Error(), "px") || !strings.Contains(err.Error(), "102") {
		t.Fatalf("error should name the proxy and the provider failure, got %v", err)
	}
}

func emptyStore(t *testing.T) *config.Store {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k1"]}`)
	t.Setenv("DS2API_ENV_WRITEBACK", "0")
	return config.LoadStore()
}

func TestProxyClientsForRetiresPreviousEndpointOfSameProxy(t *testing.T) {
	client := NewClient(emptyStore(t), nil)
	defer client.Close()

	first := config.Proxy{ID: "px", Type: "http", Host: "10.0.0.1", Port: 1080}
	second := config.Proxy{ID: "px", Type: "http", Host: "10.0.0.2", Port: 1080}

	if _, err := client.proxyClientsFor(first); err != nil {
		t.Fatalf("bundle for the first node: %v", err)
	}
	if _, err := client.proxyClientsFor(second); err != nil {
		t.Fatalf("bundle for the second node: %v", err)
	}

	client.proxyClientsMu.RLock()
	var live []string
	for key, entry := range client.proxyClients {
		if !entry.retiring {
			live = append(live, key)
		}
	}
	client.proxyClientsMu.RUnlock()

	if len(live) != 1 || !strings.Contains(live[0], "10.0.0.2") {
		t.Fatalf("expected only the newest endpoint to be live, got %#v", live)
	}
}

func TestSweepRetiredProxyClientsClosesIdleBundles(t *testing.T) {
	client := NewClient(emptyStore(t), nil)
	defer client.Close()

	usage := &proxyUsage{}
	bundle := trackRequestClients(requestClients{
		regular:   stubDoer{body: "x"},
		stream:    stubDoer{body: "x"},
		fallback:  stubDoer{body: "x"},
		fallbackS: stubDoer{body: "x"},
	}, usage)

	client.proxyClients["old"] = cachedProxyClients{
		proxy:     config.Proxy{ID: "px", Type: "http", Host: "10.0.0.1", Port: 1080},
		clients:   bundle,
		usage:     usage,
		retiring:  true,
		retiredAt: time.Now().Add(-time.Hour),
	}

	closed := client.sweepRetiredProxyClients(time.Now())
	if len(closed) != 1 {
		t.Fatalf("expected the idle retired bundle to be swept, got %d", len(closed))
	}
	if _, exists := client.proxyClients["old"]; exists {
		t.Fatal("expected the swept entry to be removed")
	}
}

func TestSweepKeepsBusyRetiredBundles(t *testing.T) {
	client := NewClient(emptyStore(t), nil)
	defer client.Close()

	usage := &proxyUsage{}
	usage.enter()
	client.proxyClients["old"] = cachedProxyClients{
		proxy:     config.Proxy{ID: "px", Type: "http", Host: "10.0.0.1", Port: 1080},
		clients:   requestClients{},
		usage:     usage,
		retiring:  true,
		retiredAt: time.Now(),
	}
	if closed := client.sweepRetiredProxyClients(time.Now()); len(closed) != 0 {
		t.Fatalf("a busy bundle must survive the sweep, got %d", len(closed))
	}
	if _, exists := client.proxyClients["old"]; !exists {
		t.Fatal("expected the busy bundle to stay registered")
	}
}

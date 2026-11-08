package proxyrotate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ds2api/internal/config"
)

func rotatingProxy(endpoint string, mutate func(*config.RotateProxyConfig)) config.Proxy {
	rotate := config.RotateProxyConfig{
		Provider: config.ProxyRotateProviderCustom,
		Endpoint: endpoint,
		Key:      "k",
		Protocol: "http",
	}
	if mutate != nil {
		mutate(&rotate)
	}
	proxy := config.Proxy{ID: "px-rotate", Rotate: config.NormalizeRotateProxy(&rotate)}
	return config.NormalizeProxy(proxy)
}

type providerStub struct {
	*httptest.Server
	calls  atomic.Int64
	mu     sync.Mutex
	status int
	body   string
	delay  time.Duration
}

func newProviderStub(t *testing.T) *providerStub {
	t.Helper()
	stub := &providerStub{status: http.StatusOK, body: `{"status":100,"message":"proxy nay se die sau 100s","proxyhttp":"10.0.0.1:1080::"}`}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.calls.Add(1)
		stub.mu.Lock()
		status, body, delay := stub.status, stub.body, stub.delay
		stub.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (p *providerStub) respond(status int, body string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = status
	p.body = body
}

func TestResolvePassesStaticProxiesThrough(t *testing.T) {
	manager := NewManager(nil)
	static := config.Proxy{ID: "s", Type: "socks5h", Host: "127.0.0.1", Port: 1080}
	got, err := manager.Resolve(context.Background(), static)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Type != "socks5h" || got.Host != "127.0.0.1" {
		t.Fatalf("static proxy mutated: %+v", got)
	}
}

func TestResolveReusesLeaseUntilRenewWindow(t *testing.T) {
	stub := newProviderStub(t)
	manager := NewManager(nil)
	now := time.Now()
	manager.SetClock(func() time.Time { return now })

	proxyCfg := rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) {
		c.TTLSeconds = 100
		c.MinRemainingSeconds = 20
	})

	first, err := manager.Resolve(context.Background(), proxyCfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if first.Host != "10.0.0.1" || first.Port != 1080 || first.ID != "px-rotate" {
		t.Fatalf("unexpected node: %+v", first)
	}

	// Inside the reuse window a second request must not hit the provider.
	now = now.Add(60 * time.Second)
	second, err := manager.Resolve(context.Background(), proxyCfg)
	if err != nil {
		t.Fatalf("Resolve reuse: %v", err)
	}
	if second.Host != first.Host {
		t.Fatalf("lease changed early: %v -> %v", first, second)
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}

	// Past the renew threshold the node is replaced.
	stub.respond(http.StatusOK, `{"status":100,"message":"proxy nay se die sau 100s","proxyhttp":"10.0.0.2:1080::"}`)
	now = now.Add(30 * time.Second)
	third, err := manager.Resolve(context.Background(), proxyCfg)
	if err != nil {
		t.Fatalf("Resolve rotate: %v", err)
	}
	if third.Host != "10.0.0.2" {
		t.Fatalf("expected rotated node, got %+v", third)
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
}

func TestResolveKeepsNodeWhenProviderFails(t *testing.T) {
	stub := newProviderStub(t)
	manager := NewManager(nil)
	now := time.Now()
	manager.SetClock(func() time.Time { return now })

	proxyCfg := rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) {
		c.TTLSeconds = 100
		c.MinRemainingSeconds = 20
	})
	if _, err := manager.Resolve(context.Background(), proxyCfg); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}

	stub.respond(http.StatusOK, `{"status":102,"message":"key khong ton tai"}`)
	now = now.Add(90 * time.Second) // past the renew window, still inside the node lifetime
	got, err := manager.Resolve(context.Background(), proxyCfg)
	if err != nil {
		t.Fatalf("Resolve during failure: %v", err)
	}
	if got.Host != "10.0.0.1" {
		t.Fatalf("expected the live node to be kept, got %+v", got)
	}

	// Once the node is truly dead the error surfaces instead of a direct call.
	now = now.Add(30 * time.Second)
	if _, err := manager.Resolve(context.Background(), proxyCfg); err == nil {
		t.Fatal("expected an error once the node expired")
	}
}

func TestResolveBackoffLimitsProviderCalls(t *testing.T) {
	stub := newProviderStub(t)
	stub.respond(http.StatusOK, `{"status":101,"comen":"key does not exist"}`)
	manager := NewManager(nil)
	now := time.Now()
	manager.SetClock(func() time.Time { return now })

	proxyCfg := rotatingProxy(stub.URL, nil)
	for i := 0; i < 5; i++ {
		if _, err := manager.Resolve(context.Background(), proxyCfg); err == nil {
			t.Fatal("expected provider error")
		}
		now = now.Add(time.Second)
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1 (backoff)", got)
	}
}

func TestResolveRefetchesWhenKeyChanges(t *testing.T) {
	stub := newProviderStub(t)
	manager := NewManager(nil)
	now := time.Now()
	manager.SetClock(func() time.Time { return now })

	proxyCfg := rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) { c.TTLSeconds = 600 })
	if _, err := manager.Resolve(context.Background(), proxyCfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	updated := rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) {
		c.TTLSeconds = 600
		c.Key = "other-key"
	})
	updated.ID = proxyCfg.ID
	if _, err := manager.Resolve(context.Background(), updated); err != nil {
		t.Fatalf("Resolve after key change: %v", err)
	}
	if got := stub.calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
}

func TestResolveSerializesConcurrentFetches(t *testing.T) {
	stub := newProviderStub(t)
	stub.mu.Lock()
	stub.delay = 40 * time.Millisecond
	stub.mu.Unlock()

	manager := NewManager(nil)
	proxyCfg := rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) { c.TTLSeconds = 600 })

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = manager.Resolve(context.Background(), proxyCfg)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1 (single flight)", got)
	}
}

func TestForceRefreshFetchesANewNode(t *testing.T) {
	stub := newProviderStub(t)
	manager := NewManager(nil)
	proxyCfg := rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) { c.TTLSeconds = 600 })

	if _, err := manager.Resolve(context.Background(), proxyCfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	stub.respond(http.StatusOK, `{"status":100,"message":"proxy nay se die sau 600s","proxyhttp":"10.0.0.9:1080::"}`)
	got, err := manager.ForceRefresh(context.Background(), proxyCfg)
	if err != nil {
		t.Fatalf("ForceRefresh: %v", err)
	}
	if got.Host != "10.0.0.9" {
		t.Fatalf("expected a fresh node, got %+v", got)
	}
	if stub.calls.Load() != 2 {
		t.Fatalf("provider calls = %d, want 2", stub.calls.Load())
	}
}

func TestStatusExposesLiveLease(t *testing.T) {
	stub := newProviderStub(t)
	manager := NewManager(nil)
	proxyCfg := rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) { c.TTLSeconds = 600 })
	if _, err := manager.Resolve(context.Background(), proxyCfg); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	lease, ok := manager.Status("px-rotate")
	if !ok {
		t.Fatal("expected status for a resolved proxy")
	}
	if lease.Host != "10.0.0.1" || lease.Port != 1080 || lease.Type != "http" {
		t.Fatalf("unexpected lease: %+v", lease)
	}
	if lease.RemainingSecond <= 0 || lease.RemainingSecond > 600 {
		t.Fatalf("unexpected remaining: %d", lease.RemainingSecond)
	}
	if lease.FetchCount != 1 {
		t.Fatalf("fetch count = %d, want 1", lease.FetchCount)
	}
	if _, ok := manager.Status("missing"); ok {
		t.Fatal("did not expect status for an unknown proxy")
	}

	statuses := manager.Statuses()
	if _, ok := statuses["px-rotate"]; !ok {
		t.Fatalf("Statuses missing entry: %v", statuses)
	}

	manager.Forget("px-rotate")
	if _, ok := manager.Status("px-rotate"); ok {
		t.Fatal("expected Forget to drop the lease")
	}
}

func TestResolveRejectsInvalidRotateConfig(t *testing.T) {
	manager := NewManager(nil)
	proxyCfg := rotatingProxy("not a url", nil)
	if _, err := manager.Resolve(context.Background(), proxyCfg); err == nil {
		t.Fatal("expected validation error")
	} else if !strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveProxyIDUsesConfiguredEntries(t *testing.T) {
	stub := newProviderStub(t)
	manager := NewManager(nil)
	cfg := config.Config{Proxies: []config.Proxy{rotatingProxy(stub.URL, func(c *config.RotateProxyConfig) { c.TTLSeconds = 300 })}}

	resolved, found, err := manager.ResolveProxyID(context.Background(), cfg, "px-rotate")
	if err != nil {
		t.Fatalf("ResolveProxyID: %v", err)
	}
	if !found || resolved.Host != "10.0.0.1" {
		t.Fatalf("unexpected result: %+v found=%v", resolved, found)
	}
	if _, found, err := manager.ResolveProxyID(context.Background(), cfg, "nope"); err != nil || found {
		t.Fatalf("unknown id: found=%v err=%v", found, err)
	}
}

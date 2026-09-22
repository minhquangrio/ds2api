package geminiweb

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"ds2api/internal/config"
)

type dummyStore struct {
	updatedCookies map[string]string
}

func (d *dummyStore) UpdateAccountCookies(id, cookies string) error {
	if d.updatedCookies == nil {
		d.updatedCookies = make(map[string]string)
	}
	d.updatedCookies[id] = cookies
	return nil
}

func (d *dummyStore) Snapshot() config.Config {
	return config.Config{
		Proxies: []config.Proxy{
			{
				ID:   "p1",
				Type: "socks5",
				Host: "127.0.0.1",
				Port: 1080,
			},
			{
				ID:   "p2",
				Type: "http",
				Host: "127.0.0.1",
				Port: 8080,
			},
		},
	}
}

func mockClientFactory(ctx context.Context, acc config.Account, opts ClientOptions, store any) (*Client, error) {
	return NewClient(acc.Cookies, ClientOptions{
		Proxy:     opts.Proxy,
		ProxyID:   opts.ProxyID,
		RotateURL: "http://127.0.0.1/rotate",
		AppURL:    "http://127.0.0.1/app",
	})
}

func TestFormatProxyURL(t *testing.T) {
	p := config.Proxy{
		Type:     "http",
		Host:     "proxy.local",
		Port:     8080,
		Username: "user",
		Password: "password",
	}
	u := FormatProxyURL(p)
	expected := "http://user:password@proxy.local:8080"
	if u != expected {
		t.Fatalf("expected %s, got %s", expected, u)
	}

	p2 := config.Proxy{
		Type: "socks5h",
		Host: "127.0.0.1",
		Port: 9050,
	}
	u2 := FormatProxyURL(p2)
	expected2 := "socks5://127.0.0.1:9050"
	if u2 != expected2 {
		t.Fatalf("expected %s, got %s", expected2, u2)
	}
}

func TestRuntimeGetClientValidation(t *testing.T) {
	rt := &Runtime{clients: make(map[string]*Client)}
	store := &dummyStore{}
	acc := config.Account{
		Email:    "test@example.com",
		Provider: "gemini",
		Cookies:  "",
	}
	_, err := rt.GetClient(context.Background(), acc, store)
	if err == nil {
		t.Fatalf("expected error for empty cookies, got nil")
	}
}

func TestRuntimeGetClientFastPathHit(t *testing.T) {
	var factoryCalls atomic.Int32
	rt := &Runtime{
		clients: make(map[string]*Client),
		clientFactory: func(ctx context.Context, acc config.Account, opts ClientOptions, store any) (*Client, error) {
			factoryCalls.Add(1)
			return mockClientFactory(ctx, acc, opts, store)
		},
	}
	store := &dummyStore{}
	acc := config.Account{
		Email:    "fastpath@example.com",
		Provider: "gemini",
		Cookies:  "__Secure-1PSID=test; __Secure-1PSIDTS=test",
		ProxyID:  "p1",
	}

	c1, err := rt.GetClient(context.Background(), acc, store)
	if err != nil {
		t.Fatalf("first GetClient failed: %v", err)
	}
	if calls := factoryCalls.Load(); calls != 1 {
		t.Fatalf("expected 1 factory call, got %d", calls)
	}

	c2, err := rt.GetClient(context.Background(), acc, store)
	if err != nil {
		t.Fatalf("second GetClient failed: %v", err)
	}
	if calls := factoryCalls.Load(); calls != 1 {
		t.Fatalf("expected fast-path to avoid factory call, but got %d calls", calls)
	}
	if c1 != c2 {
		t.Fatalf("expected identical client pointer from fast-path")
	}
}

func TestRuntimeGetClientProxyChange(t *testing.T) {
	rt := &Runtime{
		clients:       make(map[string]*Client),
		clientFactory: mockClientFactory,
	}
	store := &dummyStore{}
	acc := config.Account{
		Email:    "proxychange@example.com",
		Provider: "gemini",
		Cookies:  "__Secure-1PSID=test; __Secure-1PSIDTS=test",
		ProxyID:  "p1",
	}

	c1, err := rt.GetClient(context.Background(), acc, store)
	if err != nil {
		t.Fatalf("GetClient p1 failed: %v", err)
	}
	if c1.Proxy() != "socks5://127.0.0.1:1080" {
		t.Errorf("expected socks5://127.0.0.1:1080, got %s", c1.Proxy())
	}
	if c1.ProxyID() != "p1" {
		t.Errorf("expected ProxyID p1, got %s", c1.ProxyID())
	}

	// Change proxy to p2 (HTTP 127.0.0.1:8080)
	acc.ProxyID = "p2"
	c2, err := rt.GetClient(context.Background(), acc, store)
	if err != nil {
		t.Fatalf("GetClient p2 failed: %v", err)
	}
	if c2.Proxy() != "http://127.0.0.1:8080" {
		t.Errorf("expected http://127.0.0.1:8080, got %s", c2.Proxy())
	}
	if c2.ProxyID() != "p2" {
		t.Errorf("expected ProxyID p2, got %s", c2.ProxyID())
	}
	if !c1.IsClosed() {
		t.Errorf("expected superseded client c1 to be closed")
	}
	if c2.IsClosed() {
		t.Errorf("expected active client c2 to NOT be closed")
	}
}

func TestRuntimeConcurrentDoubleCheck(t *testing.T) {
	barrier := make(chan struct{})
	var enteredBarrier sync.WaitGroup
	enteredBarrier.Add(2)

	var createdClientsMu sync.Mutex
	var createdClients []*Client

	rt := &Runtime{
		clients: make(map[string]*Client),
		clientFactory: func(ctx context.Context, acc config.Account, opts ClientOptions, store any) (*Client, error) {
			c, err := mockClientFactory(ctx, acc, opts, store)
			if err != nil {
				return nil, err
			}
			createdClientsMu.Lock()
			createdClients = append(createdClients, c)
			createdClientsMu.Unlock()

			enteredBarrier.Done()
			<-barrier // Wait for both goroutines to finish creation before entering double-check lock
			return c, nil
		},
	}
	store := &dummyStore{}
	acc := config.Account{
		Email:    "concurrent@example.com",
		Provider: "gemini",
		Cookies:  "__Secure-1PSID=test; __Secure-1PSIDTS=test",
		ProxyID:  "p1",
	}

	var wg sync.WaitGroup
	results := make([]*Client, 2)
	errs := make([]error, 2)

	for i := 0; i < 2; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[idx], errs[idx] = rt.GetClient(context.Background(), acc, store)
		}()
	}

	enteredBarrier.Wait()
	close(barrier) // Release both goroutines into double-check lock
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d failed: %v", i, err)
		}
	}

	if results[0] != results[1] {
		t.Fatalf("expected both concurrent callers to receive the same winning client pointer")
	}
	if results[0].IsClosed() {
		t.Fatalf("winning client should not be closed")
	}

	createdClientsMu.Lock()
	defer createdClientsMu.Unlock()
	if len(createdClients) != 2 {
		t.Fatalf("expected exactly 2 clients created, got %d", len(createdClients))
	}
	closedCount := 0
	for _, c := range createdClients {
		if c.IsClosed() {
			closedCount++
		}
	}
	if closedCount != 1 {
		t.Errorf("expected exactly 1 losing client closed, got %d", closedCount)
	}
}

func TestRuntimeRemoveAndResetClients(t *testing.T) {
	rt := &Runtime{
		clients:       make(map[string]*Client),
		accountEpoch:  make(map[string]uint64),
		clientFactory: mockClientFactory,
	}
	store := &dummyStore{}
	acc1 := config.Account{
		Email:    "user1@example.com",
		Provider: "gemini",
		Cookies:  "__Secure-1PSID=test1",
	}
	acc2 := config.Account{
		Email:    "user2@example.com",
		Provider: "gemini",
		Cookies:  "__Secure-1PSID=test2",
	}

	c1, err := rt.GetClient(context.Background(), acc1, store)
	if err != nil {
		t.Fatalf("GetClient acc1 failed: %v", err)
	}
	c2, err := rt.GetClient(context.Background(), acc2, store)
	if err != nil {
		t.Fatalf("GetClient acc2 failed: %v", err)
	}

	rt.RemoveClient(acc1.Identifier())
	if !c1.IsClosed() {
		t.Errorf("expected c1 to be closed after RemoveClient")
	}
	if c2.IsClosed() {
		t.Errorf("expected c2 to remain open after RemoveClient(acc1)")
	}

	// acc2 must hit fast-path and NOT be recreated just because acc1 was removed!
	c2After, err := rt.GetClient(context.Background(), acc2, store)
	if err != nil {
		t.Fatalf("GetClient acc2 after RemoveClient(acc1) failed: %v", err)
	}
	if c2After != c2 {
		t.Errorf("expected c2 to be reused without recreation when acc1 is removed")
	}

	epochBeforeReset := rt.epoch.Load()
	rt.ResetClients()
	if rt.epoch.Load() != epochBeforeReset+1 {
		t.Errorf("expected epoch to increment on ResetClients")
	}
	if !c2.IsClosed() {
		t.Errorf("expected c2 to be closed after ResetClients")
	}

	rt.mu.RLock()
	clientMapLen := len(rt.clients)
	rt.mu.RUnlock()
	if clientMapLen != 0 {
		t.Errorf("expected client map to be empty, got len %d", clientMapLen)
	}
}

func TestRuntimeSlowPathStaleRejection(t *testing.T) {
	initStarted := make(chan struct{})
	continueInit := make(chan struct{})

	rt := &Runtime{
		clients:      make(map[string]*Client),
		accountEpoch: make(map[string]uint64),
		clientFactory: func(ctx context.Context, acc config.Account, opts ClientOptions, store any) (*Client, error) {
			close(initStarted)
			<-continueInit // wait for admin action to intervene
			return mockClientFactory(ctx, acc, opts, store)
		},
	}

	store := &dummyStore{}
	acc := config.Account{
		Email:    "stale@example.com",
		Provider: "gemini",
		Cookies:  "__Secure-1PSID=test",
	}

	var resClient *Client
	var getErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		resClient, getErr = rt.GetClient(context.Background(), acc, store)
	}()

	<-initStarted
	// Admin intervenes while slow-path is initializing
	rt.RemoveClient(acc.Identifier())
	close(continueInit)
	wg.Wait()

	if getErr == nil {
		t.Fatalf("expected error due to configuration change during init, got nil")
	}
	if resClient != nil {
		t.Fatalf("expected nil client on stale init, got %v", resClient)
	}
}

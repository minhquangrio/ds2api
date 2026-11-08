package muse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"ds2api/internal/assistantturn"
	"ds2api/internal/config"
	"ds2api/internal/geminiweb"
	"ds2api/internal/promptcompat"
	"ds2api/internal/proxyrotate"
)

type StoreSnapshotter interface {
	Snapshot() config.Config
}

type MuseConfigReader interface {
	MuseWorkerURL() string
	MuseWorkerSecret() string
	MuseEnabled() bool
}

type Runtime struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

var (
	defaultRuntimeInstance *Runtime
	defaultRuntimeOnce     sync.Once
)

func DefaultRuntime() *Runtime {
	defaultRuntimeOnce.Do(func() {
		defaultRuntimeInstance = &Runtime{
			clients: make(map[string]*Client),
		}
	})
	return defaultRuntimeInstance
}

func (r *Runtime) EvictClient(accountID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, accountID)
}

func (r *Runtime) GetClient(ctx context.Context, acc config.Account, store any) (*Client, error) {
	id := acc.Identifier()
	if id == "" {
		id = acc.Name
	}
	if id == "" {
		id = "default"
	}

	workerURL := ""
	secret := ""
	proxyURL := ""

	// Check environment variables first
	if envURL := strings.TrimSpace(os.Getenv("DS2API_MUSE_WORKER_URL")); envURL != "" {
		workerURL = envURL
	}
	if envSecret := strings.TrimSpace(os.Getenv("DS2API_MUSE_WORKER_SECRET")); envSecret != "" {
		secret = envSecret
	}

	// Check store configuration
	if reader, ok := store.(MuseConfigReader); ok {
		if workerURL == "" {
			workerURL = reader.MuseWorkerURL()
		}
		if secret == "" {
			secret = reader.MuseWorkerSecret()
		}
	}
	if snap, ok := store.(StoreSnapshotter); ok {
		cfg := snap.Snapshot()
		if workerURL == "" {
			workerURL = cfg.Muse.WorkerURL
		}
		if secret == "" {
			secret = cfg.Muse.Secret
		}
		if proxyID := strings.TrimSpace(acc.ProxyID); proxyID != "" {
			resolved, found, err := proxyrotate.ResolveProxyID(ctx, cfg, proxyID)
			if err != nil {
				return nil, fmt.Errorf("muse proxy unavailable: %w", err)
			}
			if found {
				proxyURL = geminiweb.FormatProxyURL(resolved)
			}
		}
	}

	if workerURL == "" {
		workerURL = "http://127.0.0.1:8765"
	}

	r.mu.RLock()
	existing, found := r.clients[id]
	r.mu.RUnlock()

	if found && existing.WorkerURL == workerURL && existing.Secret == secret && existing.Account.Cookies == acc.Cookies && existing.ProxyURL == proxyURL {
		return existing, nil
	}

	if strings.TrimSpace(acc.Cookies) == "" {
		return nil, errors.New("muse account has no cookies configured")
	}

	client := NewClient(workerURL, secret, proxyURL, acc)
	if updater, ok := store.(interface{ UpdateAccountCookies(string, string) error }); ok {
		client.OnCookieUpdate = func(cookies string) {
			if err := updater.UpdateAccountCookies(acc.Identifier(), cookies); err != nil {
				config.Logger.Warn("[muse] cookie update failed", "account", acc.Identifier(), "error", err)
			}
		}
	}

	r.mu.Lock()
	r.clients[id] = client
	r.mu.Unlock()

	return client, nil
}

func ExecuteTurn(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (assistantturn.Turn, error) {
	if client == nil {
		return assistantturn.Turn{}, ErrMuseWorkerUnavailable
	}
	for attempt := 0; ; attempt++ {
		turn, err := client.ExecuteChat(ctx, stdReq)
		if !retryEmpty(turn, err, attempt) {
			return turn, err
		}
		stdReq = emptyRetryRequest(stdReq)
	}
}

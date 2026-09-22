package geminiweb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"ds2api/internal/assistantturn"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

type StoreSnapshotter interface {
	Snapshot() config.Config
}

type StoreCookieUpdater interface {
	UpdateAccountCookies(identifier, cookies string) error
}

type accountClient struct {
	id     string
	client *Client
}

type clientFactoryFunc func(ctx context.Context, acc config.Account, opts ClientOptions, store any) (*Client, error)

type Runtime struct {
	mu            sync.RWMutex
	clients       map[string]*Client
	accountEpoch  map[string]uint64
	epoch         atomic.Uint64
	clientFactory clientFactoryFunc
	workerMu      sync.Mutex
	workerCancel  context.CancelFunc
	workerWg      sync.WaitGroup
	workerRun     bool
}

var defaultRuntime = &Runtime{
	clients:      make(map[string]*Client),
	accountEpoch: make(map[string]uint64),
}

func DefaultRuntime() *Runtime {
	return defaultRuntime
}

func (r *Runtime) getClientFactory() clientFactoryFunc {
	if r.clientFactory != nil {
		return r.clientFactory
	}
	return defaultClientFactory
}

func defaultClientFactory(ctx context.Context, acc config.Account, opts ClientOptions, store any) (*Client, error) {
	client, err := NewClient(acc.Cookies, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	if updater, ok := store.(StoreCookieUpdater); ok {
		accountID := acc.Identifier()
		client.SetOnCookieUpdate(func(newCookies string) {
			_ = updater.UpdateAccountCookies(accountID, newCookies)
		})
	}

	if _, err := client.InitSession(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to initialize gemini session: %w", err)
	}
	return client, nil
}

func (r *Runtime) GetClient(ctx context.Context, acc config.Account, store any) (*Client, error) {
	if strings.TrimSpace(acc.Cookies) == "" {
		return nil, errors.New("gemini account has no cookies configured")
	}

	id := acc.Identifier()
	if id == "" {
		id = "default"
	}
	proxyID := strings.TrimSpace(acc.ProxyID)

	// 1. FAST-PATH: 0 allocation, O(1) cache hit
	r.mu.RLock()
	existing, found := r.clients[id]
	r.mu.RUnlock()
	if found && existing != nil && !existing.IsClosed() && existing.ProxyID() == proxyID {
		return existing, nil
	}

	// 2. SLOW-PATH: snapshot epoch trước khi tạo client
	r.mu.RLock()
	startEpoch := r.epoch.Load()
	var startAccEpoch uint64
	if r.accountEpoch != nil {
		startAccEpoch = r.accountEpoch[id]
	}
	r.mu.RUnlock()

	proxyURL := ""
	if proxyID != "" && store != nil {
		if snap, ok := store.(StoreSnapshotter); ok {
			cfg := snap.Snapshot()
			for _, p := range cfg.Proxies {
				if p.ID == proxyID {
					proxyURL = FormatProxyURL(p)
					break
				}
			}
		}
	}

	// 3. Khởi tạo client mới ngoài lock
	newClient, err := r.getClientFactory()(ctx, acc, ClientOptions{Proxy: proxyURL, ProxyID: proxyID}, store)
	if err != nil {
		return nil, err
	}

	// 4. Double-check dưới r.mu.Lock() kèm xác thực Epoch
	var toClose *Client
	var resultClient *Client
	var epochErr error

	r.mu.Lock()
	latestEpoch := r.epoch.Load()
	var latestAccEpoch uint64
	if r.accountEpoch != nil {
		latestAccEpoch = r.accountEpoch[id]
	}

	if latestEpoch != startEpoch || latestAccEpoch != startAccEpoch {
		toClose = newClient
		if c, ok := r.clients[id]; ok && c != nil && !c.IsClosed() && c.ProxyID() == proxyID {
			resultClient = c
		} else {
			epochErr = errors.New("gemini client configuration changed during initialization, please retry")
		}
	} else {
		existing, found = r.clients[id]
		if found && existing != nil && !existing.IsClosed() && existing.ProxyID() == proxyID {
			toClose = newClient
			resultClient = existing
		} else {
			if r.clients == nil {
				r.clients = make(map[string]*Client)
			}
			r.clients[id] = newClient
			resultClient = newClient
			if found && existing != nil {
				toClose = existing
			}
		}
	}
	r.mu.Unlock()

	// 5. Đóng socket ngoài lock, ghi log lỗi nếu có
	if toClose != nil {
		if cErr := toClose.Close(); cErr != nil {
			config.Logger.Warn("[geminiweb] error closing superseded client", "account", id, "error", cErr)
		}
	}
	if epochErr != nil {
		return nil, epochErr
	}
	return resultClient, nil
}

func FormatProxyURL(p config.Proxy) string {
	p = config.NormalizeProxy(p)
	scheme := strings.ToLower(strings.TrimSpace(p.Type))
	if scheme == "socks5h" || scheme == "" {
		scheme = "socks5"
	}
	u := &url.URL{
		Scheme: scheme,
		Host:   fmt.Sprintf("%s:%d", p.Host, p.Port),
	}
	if p.Username != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

func ExecuteTurn(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (assistantturn.Turn, error) {
	prompt := stdReq.PromptTokenText
	if prompt == "" {
		prompt = stdReq.FinalPrompt
	}

	opts := GenerateOptions{
		Model:    stdReq.ResolvedModel,
		Thinking: stdReq.Thinking,
		FileIDs:  stdReq.RefFileIDs,
	}
	if opts.Model == "" {
		opts.Model = stdReq.RequestedModel
	}

	res, err := client.GenerateWithRetry(ctx, prompt, opts)
	if err != nil {
		return assistantturn.Turn{}, err
	}

	inputTokens := len(prompt)/4 + 1
	outputTokens := len(res.Text)/4 + 1
	reasoningTokens := len(res.Thoughts) / 4
	totalTokens := inputTokens + outputTokens + reasoningTokens

	return assistantturn.Turn{
		Model:       stdReq.ResponseModel,
		Prompt:      prompt,
		Text:        res.Text,
		RawText:     res.Text,
		Thinking:    res.Thoughts,
		RawThinking: res.Thoughts,
		StopReason:  assistantturn.StopReasonStop,
		Usage: assistantturn.Usage{
			InputTokens:     inputTokens,
			OutputTokens:    outputTokens,
			ReasoningTokens: reasoningTokens,
			TotalTokens:     totalTokens,
		},
	}, nil
}

func (r *Runtime) takeAllClientsLocked() []accountClient {
	out := make([]accountClient, 0, len(r.clients))
	for id, c := range r.clients {
		out = append(out, accountClient{id: id, client: c})
		delete(r.clients, id)
	}
	return out
}

func closeClientsList(items []accountClient) {
	for _, item := range items {
		if item.client != nil {
			if err := item.client.Close(); err != nil {
				config.Logger.Warn("[geminiweb] error closing client during teardown", "account", item.id, "error", err)
			}
		}
	}
}

func (r *Runtime) RemoveClient(id string) {
	r.mu.Lock()
	if r.accountEpoch == nil {
		r.accountEpoch = make(map[string]uint64)
	}
	r.accountEpoch[id]++
	client, found := r.clients[id]
	if found {
		delete(r.clients, id)
	}
	r.mu.Unlock()
	if found && client != nil {
		if err := client.Close(); err != nil {
			config.Logger.Warn("[geminiweb] error closing removed client", "account", id, "error", err)
		}
	}
}

func (r *Runtime) ResetClients() {
	r.epoch.Add(1)
	r.mu.Lock()
	items := r.takeAllClientsLocked()
	r.mu.Unlock()
	closeClientsList(items)
}

func (r *Runtime) Close() error {
	r.StopBackgroundRefresher()
	r.epoch.Add(1)
	r.mu.Lock()
	items := r.takeAllClientsLocked()
	r.mu.Unlock()
	closeClientsList(items)
	return nil
}

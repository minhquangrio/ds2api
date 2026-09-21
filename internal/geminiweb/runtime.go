package geminiweb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

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

type Runtime struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

var defaultRuntime = &Runtime{
	clients: make(map[string]*Client),
}

func DefaultRuntime() *Runtime {
	return defaultRuntime
}

func (r *Runtime) GetClient(ctx context.Context, acc config.Account, store any) (*Client, error) {
	if strings.TrimSpace(acc.Cookies) == "" {
		return nil, errors.New("gemini account has no cookies configured")
	}

	id := acc.Identifier()
	if id == "" {
		id = "default"
	}

	r.mu.RLock()
	existing, found := r.clients[id]
	r.mu.RUnlock()
	if found && !existing.closed {
		return existing, nil
	}

	proxyURL := ""
	if proxyID := strings.TrimSpace(acc.ProxyID); proxyID != "" && store != nil {
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

	client, err := NewClient(acc.Cookies, ClientOptions{
		Proxy: proxyURL,
	})
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

	r.mu.Lock()
	if existing, found := r.clients[id]; found && !existing.closed {
		r.mu.Unlock()
		_ = client.Close()
		return existing, nil
	}
	r.clients[id] = client
	r.mu.Unlock()

	return client, nil
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

	res, err := client.Generate(ctx, prompt, opts)
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

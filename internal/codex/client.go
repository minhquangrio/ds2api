package codex

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type Client struct {
	mu           sync.RWMutex
	token        string
	accountID    string
	proxyURL     string
	defaultModel string
	httpClient   *http.Client
}

func NewClient(token, accountID, proxyURL string) *Client {
	c := &Client{
		token:     token,
		accountID: accountID,
		proxyURL:  proxyURL,
	}
	c.initHTTPClient()
	return c
}

func (c *Client) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

func (c *Client) SetDefaultModel(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultModel = model
}

// DefaultModel is used when a request resolves to no model at all, so an empty
// `model` field is never forwarded upstream.
func (c *Client) DefaultModel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.defaultModel
}

// buildTransport returns a transport that routes through proxyURL when set.
// Shared by the API client and the OAuth token calls so a proxied deployment
// reaches auth.openai.com the same way it reaches the model API.
func buildTransport(proxyURL string) *http.Transport {
	transport := &http.Transport{
		MaxIdleConns:        50,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	if strings.TrimSpace(proxyURL) == "" {
		return transport
	}

	u, err := url.Parse(proxyURL)
	if err != nil {
		log.Printf("codex: invalid proxy url: %v", err)
		return transport
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme == "http" || scheme == "https" {
		transport.Proxy = http.ProxyURL(u)
		return transport
	}

	// SOCKS5
	var authCfg *proxy.Auth
	if u.User != nil {
		pwd, _ := u.User.Password()
		authCfg = &proxy.Auth{
			User:     u.User.Username(),
			Password: pwd,
		}
	}
	dialer, err := proxy.SOCKS5("tcp", u.Host, authCfg, &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	if err != nil {
		log.Printf("codex: failed to initialize proxy dialer: %v", err)
		return transport
	}
	if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
		transport.DialContext = ctxDialer.DialContext
	} else {
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	}
	return transport
}

func (c *Client) initHTTPClient() {
	c.httpClient = &http.Client{
		Transport: buildTransport(c.proxyURL),
	}
}

func (c *Client) prepareRequest(req *http.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set(HeaderOpenAIBeta, HeaderOpenAIBetaVal)
	req.Header.Set(HeaderOriginator, HeaderOriginatorVal)
	if c.accountID != "" {
		req.Header.Set(HeaderChatGPTAccount, c.accountID)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36")
	}
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	c.prepareRequest(req)
	return c.httpClient.Do(req)
}

func (c *Client) DoStream(req *http.Request) (*http.Response, error) {
	c.prepareRequest(req)
	req.Header.Set("Accept", "text/event-stream")
	return c.httpClient.Do(req)
}

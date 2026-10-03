package netdiag

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"ds2api/internal/config"
	"ds2api/internal/geminiweb"
)

type IPInfo struct {
	IP        string `json:"ip,omitempty"`
	Country   string `json:"country,omitempty"`
	City      string `json:"city,omitempty"`
	Colo      string `json:"colo,omitempty"`
	Provider  string `json:"provider,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
}

type PlatformStatus struct {
	Platform   string `json:"platform"`
	URL        string `json:"url"`
	Status     string `json:"status"` // "reachable", "limited", "unavailable"
	HTTPStatus int    `json:"http_status,omitempty"`
	ElapsedMs  int64  `json:"elapsed_ms"`
	Error      string `json:"error,omitempty"`
}

type Report struct {
	IPv4      *IPInfo          `json:"ipv4,omitempty"`
	IPv6      *IPInfo          `json:"ipv6,omitempty"`
	DNS       []string         `json:"dns"`
	Platforms []PlatformStatus `json:"platforms"`
	OS        string           `json:"os,omitempty"`
	ProxyUsed bool             `json:"proxy_used"`
	Timestamp int64            `json:"timestamp"`
}

func CollectReport(ctx context.Context, proxyCfg *config.Proxy) Report {
	var rep Report
	rep.Timestamp = time.Now().Unix()
	rep.OS = runtime.GOOS
	if proxyCfg != nil && proxyCfg.Host != "" {
		rep.ProxyUsed = true
	}

	httpClient := buildHTTPClient(proxyCfg, 5*time.Second)

	var wg sync.WaitGroup
	wg.Add(4)

	// 1. IPv4 + Trace
	go func() {
		defer wg.Done()
		rep.IPv4 = detectIPv4(ctx, httpClient)
	}()

	// 2. IPv6
	go func() {
		defer wg.Done()
		rep.IPv6 = detectIPv6(ctx, httpClient)
	}()

	// 3. DNS
	go func() {
		defer wg.Done()
		rep.DNS = DetectDNSServers(ctx)
	}()

	// 4. Platforms
	go func() {
		defer wg.Done()
		rep.Platforms = CheckPlatforms(ctx, httpClient)
	}()

	wg.Wait()
	if rep.DNS == nil {
		rep.DNS = []string{}
	}
	if rep.Platforms == nil {
		rep.Platforms = []PlatformStatus{}
	}
	return rep
}

func buildHTTPClient(proxyCfg *config.Proxy, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     15 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}

	if proxyCfg != nil && proxyCfg.Host != "" {
		norm := config.NormalizeProxy(*proxyCfg)
		scheme := strings.ToLower(norm.Type)
		if scheme == "http" || scheme == "https" {
			proxyURL, err := url.Parse(geminiweb.FormatProxyURL(norm))
			if err == nil {
				transport.Proxy = http.ProxyURL(proxyURL)
			}
		} else {
			var authCfg *proxy.Auth
			if norm.Username != "" || norm.Password != "" {
				authCfg = &proxy.Auth{User: norm.Username, Password: norm.Password}
			}
			dialer, err := proxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", norm.Host, norm.Port), authCfg, &net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 15 * time.Second,
			})
			if err == nil {
				if ctxDialer, ok := dialer.(proxy.ContextDialer); ok {
					transport.DialContext = ctxDialer.DialContext
				} else {
					transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
						return dialer.Dial(network, addr)
					}
				}
			} else {
				log.Printf("netdiag: failed to initialize socks5 dialer: %v", err)
			}
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

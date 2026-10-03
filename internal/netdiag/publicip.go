package netdiag

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ipv4Endpoints = []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
		"https://checkip.amazonaws.com",
	}

	ipv6Endpoints = []string{
		"https://v6.ident.me",
		"https://ipv6.icanhazip.com",
		"https://api64.ipify.org",
	}

	cfTraceURL = "https://www.cloudflare.com/cdn-cgi/trace"
)

// queryFirstString races the endpoints and returns the first valid answer along
// with the host that produced it, so the report can show which service answered.
func queryFirstString(ctx context.Context, client *http.Client, endpoints []string, validator func(string) bool) (string, string) {
	type result struct {
		val    string
		source string
	}
	resCh := make(chan result, len(endpoints))
	ctxSub, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for _, ep := range endpoints {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctxSub, http.MethodGet, url, nil)
			if err != nil {
				return
			}
			req.Header.Set("User-Agent", "curl/8.0.0")

			resp, err := client.Do(req)
			if err != nil {
				return
			}
			defer func() {
				if cerr := resp.Body.Close(); cerr != nil {
					log.Printf("netdiag: failed to close response body from %s: %v", url, cerr)
				}
			}()

			if resp.StatusCode != http.StatusOK {
				return
			}

			body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
			if err != nil {
				return
			}
			text := strings.TrimSpace(string(body))
			if validator == nil || validator(text) {
				select {
				case resCh <- result{val: text, source: endpointHost(url)}:
					cancel()
				default:
				}
			}
		}(ep)
	}

	go func() {
		wg.Wait()
		close(resCh)
	}()

	select {
	case r, ok := <-resCh:
		if ok && r.val != "" {
			return r.val, r.source
		}
	case <-ctx.Done():
	}
	return "", ""
}

func endpointHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Host
}

func parseCloudflareTrace(reader io.Reader) map[string]string {
	data := make(map[string]string)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			data[parts[0]] = parts[1]
		}
	}
	return data
}

func fetchCloudflareTrace(ctx context.Context, client *http.Client) map[string]string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfTraceURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "curl/8.0.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("netdiag: failed to close cf trace body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil
	}
	return parseCloudflareTrace(resp.Body)
}

func detectIPv4(ctx context.Context, client *http.Client) *IPInfo {
	ctxTimeout, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	start := time.Now()
	var ip, source string
	var trace map[string]string
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		ip, source = queryFirstString(ctxTimeout, client, ipv4Endpoints, func(s string) bool {
			parsed := net.ParseIP(s)
			return parsed != nil && parsed.To4() != nil
		})
	}()

	go func() {
		defer wg.Done()
		trace = fetchCloudflareTrace(ctxTimeout, client)
	}()

	wg.Wait()

	if ip == "" && trace["ip"] != "" {
		parsed := net.ParseIP(trace["ip"])
		if parsed != nil && parsed.To4() != nil {
			ip = trace["ip"]
			source = "cloudflare.com"
		}
	}

	if ip == "" && len(trace) == 0 {
		return nil
	}

	info := &IPInfo{
		IP:        ip,
		Provider:  source,
		LatencyMs: time.Since(start).Milliseconds(),
	}
	if trace != nil {
		info.Country = trace["loc"]
		info.Colo = trace["colo"]
		if info.Provider == "" {
			info.Provider = "cloudflare.com"
		}
	}
	return info
}

func detectIPv6(ctx context.Context, client *http.Client) *IPInfo {
	ctxTimeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	start := time.Now()
	ip, source := queryFirstString(ctxTimeout, client, ipv6Endpoints, func(s string) bool {
		parsed := net.ParseIP(s)
		return parsed != nil && parsed.To4() == nil && strings.Contains(s, ":")
	})

	if ip == "" {
		return nil
	}

	return &IPInfo{
		IP:        ip,
		Provider:  source,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

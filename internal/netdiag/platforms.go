package netdiag

import (
	"context"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type platformTarget struct {
	Platform string
	URL      string
}

var defaultTargets = []platformTarget{
	{Platform: "DeepSeek", URL: "https://chat.deepseek.com/"},
	{Platform: "Gemini", URL: "https://gemini.google.com/"},
	{Platform: "Google", URL: "https://www.google.com/generate_204"},
	{Platform: "Cloudflare", URL: "https://www.cloudflare.com/cdn-cgi/trace"},
}

func CheckPlatforms(ctx context.Context, client *http.Client) []PlatformStatus {
	ctxTimeout, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	results := make([]PlatformStatus, len(defaultTargets))
	var wg sync.WaitGroup

	for i, target := range defaultTargets {
		wg.Add(1)
		go func(index int, tgt platformTarget) {
			defer wg.Done()
			results[index] = checkSinglePlatform(ctxTimeout, client, tgt)
		}(i, target)
	}

	wg.Wait()
	return results
}

func checkSinglePlatform(ctx context.Context, client *http.Client, target platformTarget) PlatformStatus {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		return PlatformStatus{
			Platform: target.Platform,
			URL:      target.URL,
			Status:   "unavailable",
			Error:    err.Error(),
		}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36")

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Milliseconds()

	if err != nil {
		return PlatformStatus{
			Platform:  target.Platform,
			URL:       target.URL,
			Status:    "unavailable",
			ElapsedMs: elapsed,
			Error:     err.Error(),
		}
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("netdiag: failed to close response body for platform %s: %v", target.Platform, cerr)
		}
	}()

	if _, err := io.CopyN(io.Discard, resp.Body, 1024); err != nil && err != io.EOF {
		log.Printf("netdiag: failed to discard body for platform %s: %v", target.Platform, err)
	}

	status := "unavailable"
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		status = "reachable"
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		status = "limited"
	}

	return PlatformStatus{
		Platform:   target.Platform,
		URL:        target.URL,
		Status:     status,
		HTTPStatus: resp.StatusCode,
		ElapsedMs:  elapsed,
	}
}

package codex

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"ds2api/internal/config"
)

func DiscoverModels(ctx context.Context, client *Client) ([]config.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CodexModelsURL, nil)
	if err != nil {
		return config.CodexModels, nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return config.CodexModels, nil
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close models response body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return config.CodexModels, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return config.CodexModels, nil
	}

	var raw struct {
		Models []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &raw); err != nil || len(raw.Models) == 0 {
		return config.CodexModels, nil
	}

	var models []config.ModelInfo
	for _, m := range raw.Models {
		models = append(models, config.ModelInfo{
			ID:      m.ID,
			Object:  "model",
			OwnedBy: "openai",
		})
	}

	return models, nil
}

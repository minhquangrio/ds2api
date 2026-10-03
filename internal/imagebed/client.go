package imagebed

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

var (
	githubAPIBase = "https://api.github.com"
	clientTimeout = 20 * time.Second
)

type githubRepoResponse struct {
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	Message       string `json:"message"`
}

type githubContentResponse struct {
	Content struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		SHA         string `json:"sha"`
		DownloadURL string `json:"download_url"`
	} `json:"content"`
	Message string `json:"message"`
}

func sanitizeConfig(cfg Config) Config {
	cfg.Owner = strings.TrimSpace(cfg.Owner)
	cfg.Repository = strings.TrimSpace(cfg.Repository)
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.PathPrefix = strings.Trim(strings.TrimSpace(cfg.PathPrefix), "/")
	if cfg.PathPrefix == "" {
		cfg.PathPrefix = "images"
	}
	cfg.Branch = strings.TrimSpace(cfg.Branch)
	return cfg
}

// resolveBranch returns the branch to write to. An empty or "auto" value defers
// to the repository's own default branch, so repositories that still use
// "master" (or anything else) work without the user having to know the name.
func resolveBranch(ctx context.Context, cfg Config, repo *githubRepoResponse) string {
	if cfg.Branch != "" && !strings.EqualFold(cfg.Branch, "auto") {
		return cfg.Branch
	}
	if repo != nil && strings.TrimSpace(repo.DefaultBranch) != "" {
		return strings.TrimSpace(repo.DefaultBranch)
	}
	return "main"
}

func resolveBranchForRepo(ctx context.Context, cfg Config) string {
	if cfg.Branch != "" && !strings.EqualFold(cfg.Branch, "auto") {
		return cfg.Branch
	}
	repo, err := EnsureRepository(ctx, cfg)
	if err != nil {
		return "main"
	}
	return resolveBranch(ctx, cfg, repo)
}

// encodeContentPath percent-encodes each path segment, so prefixes or file names
// containing spaces or reserved characters cannot break the request URL.
func encodeContentPath(p string) string {
	segments := strings.Split(p, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

func setGitHubHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "DS2API-ImageBed")
}

func EnsureRepository(ctx context.Context, cfg Config) (*githubRepoResponse, error) {
	cfg = sanitizeConfig(cfg)
	if cfg.Token == "" {
		return nil, errors.New("GitHub personal access token is required")
	}
	if cfg.Owner == "" || cfg.Repository == "" {
		return nil, errors.New("GitHub repository owner and name are required")
	}

	repoURL := fmt.Sprintf("%s/repos/%s/%s", githubAPIBase, url.PathEscape(cfg.Owner), url.PathEscape(cfg.Repository))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repoURL, nil)
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, cfg.Token)

	client := &http.Client{Timeout: clientTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to GitHub API: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("imagebed: failed to close response body: %v", cerr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read GitHub response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository %s/%s not found (ensure repo exists and token has access)", cfg.Owner, cfg.Repository)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("GitHub token is invalid or expired")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub API error (%d): %s", resp.StatusCode, string(body))
	}

	var repoInfo githubRepoResponse
	if err := json.Unmarshal(body, &repoInfo); err != nil {
		return nil, fmt.Errorf("parse repo info: %w", err)
	}

	if repoInfo.Private {
		return &repoInfo, errors.New("repository is private; public image hosting requires a public repository")
	}

	return &repoInfo, nil
}

func UploadImage(ctx context.Context, cfg Config, dataURL, originalName string) (*HistoryItem, error) {
	cfg = sanitizeConfig(cfg)
	if cfg.Token == "" || cfg.Owner == "" || cfg.Repository == "" {
		return nil, errors.New("incomplete GitHub image bed configuration")
	}

	mimeType, b64Payload, err := ParseDataURL(dataURL)
	if err != nil {
		return nil, err
	}

	rawBytes, err := base64.StdEncoding.DecodeString(b64Payload)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 payload: %w", err)
	}
	size := int64(len(rawBytes))

	targetPath := MakeImagePath(cfg.PathPrefix, originalName, mimeType, time.Now())
	branch := resolveBranchForRepo(ctx, cfg)

	reqBody := map[string]string{
		"message": "Upload " + filepath.Base(targetPath),
		"content": b64Payload,
		"branch":  branch,
	}
	payloadBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	uploadURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s",
		githubAPIBase, url.PathEscape(cfg.Owner), url.PathEscape(cfg.Repository), encodeContentPath(targetPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	setGitHubHeaders(req, cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload to GitHub: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("imagebed: failed to close upload response body: %v", cerr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read upload response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub upload failed (%d): %s", resp.StatusCode, string(body))
	}

	var contentResp githubContentResponse
	if err := json.Unmarshal(body, &contentResp); err != nil {
		return nil, fmt.Errorf("parse upload response: %w", err)
	}

	imgURL := contentResp.Content.DownloadURL
	if imgURL == "" {
		imgURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
			url.PathEscape(cfg.Owner), url.PathEscape(cfg.Repository), url.PathEscape(branch), encodeContentPath(targetPath))
	}

	item := HistoryItem{
		ID:        GenerateUUID8(),
		FileName:  filepath.Base(targetPath),
		Path:      targetPath,
		SHA:       contentResp.Content.SHA,
		URL:       imgURL,
		Size:      size,
		CreatedAt: time.Now(),
	}

	if err := AddHistoryItem(item); err != nil {
		log.Printf("imagebed: failed to save history: %v", err)
	}

	return &item, nil
}

func DeleteRemoteFile(ctx context.Context, cfg Config, path, sha string) error {
	cfg = sanitizeConfig(cfg)
	if cfg.Token == "" || cfg.Owner == "" || cfg.Repository == "" {
		return errors.New("incomplete GitHub image bed configuration")
	}

	branch := resolveBranchForRepo(ctx, cfg)

	client := &http.Client{Timeout: clientTimeout}

	if sha == "" {
		lookupURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
			githubAPIBase, url.PathEscape(cfg.Owner), url.PathEscape(cfg.Repository), encodeContentPath(path), url.QueryEscape(branch))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
		if err != nil {
			return err
		}
		setGitHubHeaders(req, cfg.Token)
		resp, err := client.Do(req)
		if err == nil {
			defer func() {
				if cerr := resp.Body.Close(); cerr != nil {
					log.Printf("imagebed: failed to close response body: %v", cerr)
				}
			}()
			if resp.StatusCode == http.StatusOK {
				var info struct {
					SHA string `json:"sha"`
				}
				if body, rerr := io.ReadAll(io.LimitReader(resp.Body, 64*1024)); rerr == nil {
					_ = json.Unmarshal(body, &info)
					sha = info.SHA
				}
			}
		}
	}

	if sha == "" {
		return errors.New("unable to retrieve file SHA for deletion")
	}

	deleteURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s",
		githubAPIBase, url.PathEscape(cfg.Owner), url.PathEscape(cfg.Repository), encodeContentPath(path))
	reqBody := map[string]string{
		"message": "Delete " + filepath.Base(path),
		"sha":     sha,
		"branch":  branch,
	}
	payloadBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return err
	}
	setGitHubHeaders(req, cfg.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete from GitHub: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("imagebed: failed to close delete response body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return fmt.Errorf("GitHub delete failed (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

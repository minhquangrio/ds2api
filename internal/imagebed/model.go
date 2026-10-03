package imagebed

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	dataURLRegex      = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)
	safeFileNameRegex = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

type Config struct {
	Token      string `json:"token"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	PathPrefix string `json:"path_prefix"`
	Branch     string `json:"branch"`
}

type ConfigResponse struct {
	HasToken   bool   `json:"has_token"`
	TokenMask  string `json:"token_mask"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	PathPrefix string `json:"path_prefix"`
	Branch     string `json:"branch"`
}

type HistoryItem struct {
	ID        string    `json:"id"`
	FileName  string    `json:"file_name"`
	Path      string    `json:"path"`
	SHA       string    `json:"sha,omitempty"`
	URL       string    `json:"url"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

func SanitizeFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "image"
	}
	name = filepath.Base(name)
	name = safeFileNameRegex.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._-")
	if name == "" {
		name = "image"
	}
	return name
}

func GenerateUUID8() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(b)
}

func ParseDataURL(dataURL string) (mimeType string, base64Content string, err error) {
	matches := dataURLRegex.FindStringSubmatch(strings.TrimSpace(dataURL))
	if len(matches) != 3 {
		return "", "", errors.New("invalid data URL format: expected data:<mime>;base64,<payload>")
	}
	return matches[1], matches[2], nil
}

func MakeImagePath(prefix, fileName, mimeType string, t time.Time) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		prefix = "images"
	}
	safe := SanitizeFileName(fileName)
	ext := filepath.Ext(safe)
	if ext == "" && mimeType != "" {
		exts, _ := mime.ExtensionsByType(mimeType)
		if len(exts) > 0 {
			ext = exts[0]
			safe += ext
		}
	}

	uuid8 := GenerateUUID8()
	return fmt.Sprintf("%s/%04d/%02d/%02d/%d-%s-%s",
		prefix, t.Year(), t.Month(), t.Day(), t.Unix(), uuid8, safe)
}

// Package mediacompat normalizes image and video requests into one project
// standard media model, the same boundary role promptcompat plays for text.
//
// Protocol adapters (OpenAI images/videos today) decode their wire shape here,
// shared policy runs once, and each provider renders back at its own boundary.
// Media deliberately does NOT travel through promptcompat.StandardRequest or
// assistantturn.Turn: those types treat empty visible text as an upstream
// failure, which is wrong for a binary artifact, and their tool/usage policies
// do not apply. Keeping a sibling model avoids smuggling images through text
// semantics (see docs/prompt-compatibility.md §17).
package mediacompat

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Surfaces recorded in history and the usage ledger.
const (
	SurfaceImagesGeneration = "openai.images_generation"
	SurfaceImagesEdit       = "openai.images_edit"
	SurfaceVideosCreate     = "openai.videos_create"
)

// Response formats supported by the media transports.
const (
	ResponseFormatB64JSON = "b64_json"
	ResponseFormatURL     = "url"
)

// MaxImagesPerRequest mirrors the worker ceiling (MUSE_IMAGE_MAX_N). Keeping
// both ends explicit means an over-large n fails at the API boundary instead
// of after an upstream generation was already billed.
const MaxImagesPerRequest = 4

// MaxReferenceBytes caps one uploaded reference before it reaches the worker.
const MaxReferenceBytes = 16 << 20

var ErrInvalidMediaRequest = errors.New("invalid media request")

var imageSizePattern = regexp.MustCompile(`^\d{2,5}\s*[x×]\s*\d{2,5}$`)

// Reference is an uploaded media input carried in memory, never a DeepSeek
// file id or token.
type Reference struct {
	Filename    string
	ContentType string
	Data        []byte
}

// ImageRequest is the normalized form of /v1/images/generations and
// /v1/images/edits.
type ImageRequest struct {
	Surface          string
	RequestedModel   string
	ResolvedModel    string
	ResponseModel    string
	Provider         string
	Prompt           string
	N                int
	Size             string
	Quality          string
	ResponseFormat   string
	References       []Reference
	PassThrough      map[string]any
	ReferenceDataURI bool
	// CallerID scopes generated media and jobs to the API key that asked for
	// them; the worker enforces it for polling.
	CallerID string
}

// IsEdit reports whether reference media must be uploaded first.
func (r ImageRequest) IsEdit() bool { return r.Surface == SurfaceImagesEdit }

// VideoRequest is the normalized form of /v1/videos.
type VideoRequest struct {
	Surface         string
	RequestedModel  string
	ResolvedModel   string
	ResponseModel   string
	Provider        string
	Prompt          string
	DurationSeconds int
	Size            string
	References      []Reference
	PassThrough     map[string]any
	CallerID        string
}

// MediaItem is one generated artifact. B64JSON is authoritative; URL is only
// present when the operator exposed a public media base.
type MediaItem struct {
	B64JSON     string
	URL         string
	MediaID     string
	ContentType string
	Width       int
	Height      int
}

// MediaResult is the rendered result of one image operation.
type MediaResult struct {
	Model     string
	Created   int64
	Items     []MediaItem
	Usage     map[string]any
	ThreadURL string
}

// VideoTask is the polling state of an asynchronous video job.
type VideoTask struct {
	ID              string
	Status          string
	Stage           string
	Model           string
	Progress        int
	CreatedAt       float64
	UpdatedAt       float64
	Error           string
	ErrorCode       string
	MediaID         string
	ContentType     string
	SizeBytes       int64
	DurationSeconds int
}

// IsTerminal reports whether polling can stop for this task state.
func (t VideoTask) IsTerminal() bool {
	switch strings.ToLower(strings.TrimSpace(t.Status)) {
	case "completed", "failed", "cancelled", "interrupted":
		return true
	}
	return false
}

func validateReferences(references []Reference) error {
	for index, ref := range references {
		if len(ref.Data) == 0 {
			return mediaError(fmt.Sprintf("reference image %d is empty", index+1))
		}
		if len(ref.Data) > MaxReferenceBytes {
			return mediaError(fmt.Sprintf("reference image %d exceeds %d bytes", index+1, MaxReferenceBytes))
		}
		if !strings.HasPrefix(strings.ToLower(ref.ContentType), "image/") {
			return mediaError(fmt.Sprintf("reference image %d must be an image", index+1))
		}
	}
	return nil
}

func normalizeSize(size string) string {
	clean := strings.ReplaceAll(size, "×", "x")
	parts := strings.Split(clean, "x")
	if len(parts) != 2 {
		return clean
	}
	return strings.TrimSpace(parts[0]) + "x" + strings.TrimSpace(parts[1])
}

func mediaError(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidMediaRequest, message)
}

func stringField(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

func intField(value any) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case float64:
		return int(typed), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(typed))
	default:
		return 0, errors.New("not a number")
	}
}

// PublicBaseURL is the operator-configured absolute base used for url
// responses. Empty means "derive from the request", never "leak the worker
// host": media ids are always served back through DS2API.
func PublicBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("DS2API_MEDIA_PUBLIC_BASE_URL")), "/")
}

// MaxImagesPerRequestFromEnv lets an operator lower the per-call image count.
func MaxImagesPerRequestFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("DS2API_IMAGES_MAX_N"))
	if raw == "" {
		return MaxImagesPerRequest
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return MaxImagesPerRequest
	}
	if value > MaxImagesPerRequest {
		return MaxImagesPerRequest
	}
	return value
}

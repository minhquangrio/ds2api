// Normalize* helpers translate one protocol body into the shared media request.
// Validation lives here so every transport (JSON, multipart, future adapters)
// applies the same rules instead of re-deriving them.

package mediacompat

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
)

// NormalizeImageRequest converts an OpenAI-shaped images payload into the
// shared media request. Uploaded reference bytes are supplied by the protocol
// adapter (multipart form or data: URIs) because they never live in the JSON
// body itself.
func NormalizeImageRequest(store config.ModelAliasReader, payload map[string]any, surface string, references []Reference) (ImageRequest, error) {
	if payload == nil {
		return ImageRequest{}, mediaError("request body must be a JSON object")
	}
	requested := strings.TrimSpace(stringField(payload, "model"))
	if requested == "" {
		return ImageRequest{}, mediaError("model is required")
	}
	prompt := strings.TrimSpace(stringField(payload, "prompt"))
	if prompt == "" {
		return ImageRequest{}, mediaError("prompt is required")
	}

	req := ImageRequest{
		Surface:        surface,
		RequestedModel: requested,
		Prompt:         prompt,
		N:              1,
		ResponseFormat: ResponseFormatB64JSON,
		PassThrough:    map[string]any{},
		References:     references,
	}
	if target, ok := config.ResolveModelTarget(store, requested); ok && target.Provider != "" {
		req.Provider = target.Provider
		req.ResolvedModel = target.Canonical
	} else {
		req.Provider = "deepseek"
		req.ResolvedModel = strings.ToLower(requested)
	}
	req.ResponseModel = req.ResolvedModel

	if raw, ok := payload["n"]; ok {
		limit := MaxImagesPerRequestFromEnv()
		count, err := intField(raw)
		if err != nil || count < 1 || count > limit {
			return ImageRequest{}, mediaError(fmt.Sprintf("n must be between 1 and %d", limit))
		}
		req.N = count
	}
	if size := strings.TrimSpace(stringField(payload, "size")); size != "" && size != "auto" {
		if !imageSizePattern.MatchString(size) {
			return ImageRequest{}, mediaError(`size must look like "1024x1024" or "auto"`)
		}
		req.Size = normalizeSize(size)
	}
	if format := strings.TrimSpace(stringField(payload, "response_format")); format != "" {
		switch strings.ToLower(format) {
		case ResponseFormatB64JSON, ResponseFormatURL:
			req.ResponseFormat = strings.ToLower(format)
		default:
			return ImageRequest{}, mediaError("response_format must be b64_json or url")
		}
	}
	req.Quality = strings.TrimSpace(stringField(payload, "quality"))

	for key, value := range payload {
		switch key {
		case "model", "prompt", "n", "size", "response_format", "image", "file", "mask", "user":
		default:
			req.PassThrough[key] = value
		}
	}
	if err := validateReferences(req.References); err != nil {
		return ImageRequest{}, err
	}
	if req.IsEdit() && len(req.References) == 0 {
		return ImageRequest{}, mediaError("an image edit requires at least one reference image")
	}
	return req, nil
}

// NormalizeVideoRequest converts an OpenAI-shaped video payload.
func NormalizeVideoRequest(store config.ModelAliasReader, payload map[string]any, references []Reference) (VideoRequest, error) {
	if payload == nil {
		return VideoRequest{}, mediaError("request body must be a JSON object")
	}
	requested := strings.TrimSpace(stringField(payload, "model"))
	if requested == "" {
		return VideoRequest{}, mediaError("model is required")
	}
	prompt := strings.TrimSpace(stringField(payload, "prompt"))
	if prompt == "" {
		return VideoRequest{}, mediaError("prompt is required")
	}
	req := VideoRequest{
		Surface:        SurfaceVideosCreate,
		RequestedModel: requested,
		Prompt:         prompt,
		PassThrough:    map[string]any{},
		References:     references,
	}
	if target, ok := config.ResolveModelTarget(store, requested); ok && target.Provider != "" {
		req.Provider = target.Provider
		req.ResolvedModel = target.Canonical
	} else {
		req.Provider = "deepseek"
		req.ResolvedModel = strings.ToLower(requested)
	}
	req.ResponseModel = req.ResolvedModel
	req.Size = strings.TrimSpace(stringField(payload, "size"))
	if raw, ok := payload["seconds"]; ok {
		seconds, err := intField(raw)
		if err != nil || seconds < 1 || seconds > 60 {
			return VideoRequest{}, mediaError("seconds must be between 1 and 60")
		}
		req.DurationSeconds = seconds
	}
	if err := validateReferences(req.References); err != nil {
		return VideoRequest{}, err
	}
	return req, nil
}

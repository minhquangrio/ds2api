package mediacompat

import (
	"errors"
	"strings"
	"testing"
)

type aliasStore struct{ aliases map[string]string }

func (s aliasStore) ModelAliases() map[string]string { return s.aliases }

func imagePayload() map[string]any {
	return map[string]any{"model": "muse-image", "prompt": "a red fox"}
}

func TestNormalizeImageRequestAcceptsDefaults(t *testing.T) {
	req, err := NormalizeImageRequest(aliasStore{}, imagePayload(), SurfaceImagesGeneration, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Provider != "muse" || req.ResolvedModel != "muse-image" || req.ResponseModel != "muse-image" {
		t.Fatalf("model resolution: %+v", req)
	}
	if req.N != 1 || req.ResponseFormat != ResponseFormatB64JSON {
		t.Fatalf("defaults: n=%d format=%q", req.N, req.ResponseFormat)
	}
	if req.Surface != SurfaceImagesGeneration || req.IsEdit() {
		t.Fatalf("surface: %+v", req)
	}
}

func TestNormalizeImageRequestValidationMatrix(t *testing.T) {
	reference := []Reference{{Filename: "a.png", ContentType: "image/png", Data: []byte("bytes")}}
	if _, err := NormalizeImageRequest(aliasStore{}, imagePayload(), SurfaceImagesEdit, reference); err != nil {
		t.Fatalf("a valid reference should be accepted: %v", err)
	}
	cases := []struct {
		name       string
		mutate     func(map[string]any)
		surface    string
		references []Reference
		wantErr    string
	}{
		{"missing model", func(p map[string]any) { delete(p, "model") }, SurfaceImagesGeneration, nil, "model is required"},
		{"missing prompt", func(p map[string]any) { p["prompt"] = "   " }, SurfaceImagesGeneration, nil, "prompt is required"},
		{"n zero", func(p map[string]any) { p["n"] = 0 }, SurfaceImagesGeneration, nil, "n must be between"},
		{"n too high", func(p map[string]any) { p["n"] = 99 }, SurfaceImagesGeneration, nil, "n must be between"},
		{"bad size", func(p map[string]any) { p["size"] = "huge" }, SurfaceImagesGeneration, nil, "size must look like"},
		{"bad format", func(p map[string]any) { p["response_format"] = "svg" }, SurfaceImagesGeneration, nil, "response_format must be"},
		{"edit without reference", func(map[string]any) {}, SurfaceImagesEdit, nil, "requires at least one reference"},
		{"empty reference bytes", func(map[string]any) {}, SurfaceImagesEdit, []Reference{{ContentType: "image/png"}}, "is empty"},
		{"non image reference", func(map[string]any) {}, SurfaceImagesEdit, []Reference{{ContentType: "text/plain", Data: []byte("x")}}, "must be an image"},
		{"oversized reference", func(map[string]any) {}, SurfaceImagesEdit, []Reference{{ContentType: "image/png", Data: make([]byte, MaxReferenceBytes+1)}}, "exceeds"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			payload := imagePayload()
			if testCase.mutate != nil {
				testCase.mutate(payload)
			}
			_, err := NormalizeImageRequest(aliasStore{}, payload, testCase.surface, testCase.references)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, ErrInvalidMediaRequest) {
				t.Fatalf("error is not typed: %v", err)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), testCase.wantErr)
			}
		})
	}
}

func TestNormalizeImageRequestAcceptsUnicodeSizeSeparator(t *testing.T) {
	payload := imagePayload()
	payload["size"] = "1024×1024"
	req, err := NormalizeImageRequest(aliasStore{}, payload, SurfaceImagesGeneration, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Size != "1024x1024" {
		t.Fatalf("size normalization: %q", req.Size)
	}
}

func TestNormalizeImageRequestKeepsUnknownFieldsInPassThrough(t *testing.T) {
	payload := imagePayload()
	payload["seed"] = 42
	payload["metadata"] = map[string]any{"tag": "case-1"}
	req, err := NormalizeImageRequest(aliasStore{}, payload, SurfaceImagesGeneration, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.PassThrough["seed"] == nil || req.PassThrough["metadata"] == nil {
		t.Fatalf("pass-through lost fields: %+v", req.PassThrough)
	}
	if _, ok := req.PassThrough["prompt"]; ok {
		t.Fatal("handled fields must not leak into pass-through")
	}
}

func TestNormalizeVideoRequestSecondsBounds(t *testing.T) {
	payload := map[string]any{"model": "muse-video", "prompt": "orbit", "seconds": "8"}
	req, err := NormalizeVideoRequest(aliasStore{}, payload, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.DurationSeconds != 8 || req.Provider != "muse" {
		t.Fatalf("normalize: %+v", req)
	}
	for _, seconds := range []string{"0", "61", "abc"} {
		invalid := map[string]any{"model": "muse-video", "prompt": "orbit", "seconds": seconds}
		if _, err := NormalizeVideoRequest(aliasStore{}, invalid, nil); err == nil {
			t.Fatalf("seconds=%q should be rejected", seconds)
		}
	}
}

func TestVideoTaskIsTerminal(t *testing.T) {
	for _, status := range []string{"completed", "failed", "Cancelled", "interrupted"} {
		if !(VideoTask{Status: status}).IsTerminal() {
			t.Fatalf("%s should be terminal", status)
		}
	}
	for _, status := range []string{"queued", "processing", ""} {
		if (VideoTask{Status: status}).IsTerminal() {
			t.Fatalf("%s should not be terminal", status)
		}
	}
}

func TestPublicBaseURLAndMaxNFromEnv(t *testing.T) {
	t.Setenv("DS2API_MEDIA_PUBLIC_BASE_URL", "https://media.example.test/")
	if got := PublicBaseURL(); got != "https://media.example.test" {
		t.Fatalf("trailing slash must be trimmed, got %q", got)
	}
	t.Setenv("DS2API_IMAGES_MAX_N", "2")
	if got := MaxImagesPerRequestFromEnv(); got != 2 {
		t.Fatalf("max n=%d", got)
	}
	payload := imagePayload()
	payload["n"] = 3
	if _, err := NormalizeImageRequest(aliasStore{}, payload, SurfaceImagesGeneration, nil); err == nil {
		t.Fatal("n=3 should fail with a lowered ceiling")
	}
	t.Setenv("DS2API_IMAGES_MAX_N", "nonsense")
	if got := MaxImagesPerRequestFromEnv(); got != MaxImagesPerRequest {
		t.Fatalf("invalid env should fall back to the default, got %d", got)
	}
}

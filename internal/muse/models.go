package muse

import (
	"fmt"
	"strings"

	"ds2api/internal/config"
)

const (
	ModelMuseSpark = "muse-spark"
	ModelMuseChat  = "muse-chat"
	ModelMuseImage = "muse-image"
	ModelMuseVideo = "muse-video"
)

// MuseModels contains all supported Muse models.
var MuseModels = config.MuseModels

// IsSupportedMuseModel reports whether the given model is recognized as a Muse model.
func IsSupportedMuseModel(model string) bool {
	baseModel := cleanModelName(model)
	for _, m := range MuseModels {
		if m.ID == baseModel {
			return true
		}
	}
	return strings.HasPrefix(baseModel, "muse/")
}

// IsMuseImageModel reports whether the model is intended for image generation.
func IsMuseImageModel(model string) bool {
	baseModel := cleanModelName(model)
	return baseModel == ModelMuseImage || baseModel == "muse/image"
}

// IsMuseVideoModel reports whether the model is intended for video generation.
func IsMuseVideoModel(model string) bool {
	baseModel := cleanModelName(model)
	return baseModel == ModelMuseVideo || baseModel == "muse/video"
}

// DefaultModel returns the default chat model for Muse.
func DefaultModel() string {
	return ModelMuseSpark
}

func cleanModelName(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimSuffix(m, "-nothinking")
	return m
}

func validateChatModel(reqModel string) error {
	model := strings.TrimPrefix(cleanModelName(reqModel), "muse/")
	switch model {
	case "", ModelMuseSpark, ModelMuseChat, "spark", "chat":
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrMuseInvalidModel, reqModel)
	}
}

// ModelKind reports which transport a Muse model belongs to so a wrong pairing
// fails at the API boundary instead of reaching the worker.
func ModelKind(model string) string {
	base := cleanModelName(model)
	switch base {
	case ModelMuseImage, "image", "muse/image":
		return "image"
	case ModelMuseVideo, "video", "muse/video":
		return "video"
	default:
		return "chat"
	}
}

func validateImageModel(reqModel string) error {
	if ModelKind(reqModel) != "image" {
		return fmt.Errorf("%w: %s is not an image model", ErrMuseInvalidModel, reqModel)
	}
	return nil
}

func validateVideoModel(reqModel string) error {
	if ModelKind(reqModel) != "video" {
		return fmt.Errorf("%w: %s is not a video model", ErrMuseInvalidModel, reqModel)
	}
	return nil
}

// workerMediaModel resolves the model name handed to the worker media routes.
func workerMediaModel(resolved, requested string) string {
	model := strings.TrimSpace(resolved)
	if model == "" {
		model = strings.TrimSpace(requested)
	}
	if model == "" {
		return ModelMuseImage
	}
	return strings.TrimPrefix(cleanModelName(model), "muse/")
}

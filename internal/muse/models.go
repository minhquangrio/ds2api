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

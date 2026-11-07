package muse

import (
	"testing"
)

func TestMuseModels(t *testing.T) {
	if !IsSupportedMuseModel("muse-spark") {
		t.Errorf("expected muse-spark to be supported")
	}
	if !IsSupportedMuseModel("muse-chat") {
		t.Errorf("expected muse-chat to be supported")
	}
	if !IsSupportedMuseModel("muse-image") {
		t.Errorf("expected muse-image to be supported")
	}
	if !IsSupportedMuseModel("muse-video") {
		t.Errorf("expected muse-video to be supported")
	}
	if !IsSupportedMuseModel("muse/custom") {
		t.Errorf("expected muse/custom to be supported")
	}
	if IsSupportedMuseModel("unknown-model") {
		t.Errorf("expected unknown-model to not be supported")
	}

	if !IsMuseImageModel("muse-image") {
		t.Errorf("expected muse-image to be image model")
	}
	if !IsMuseVideoModel("muse-video") {
		t.Errorf("expected muse-video to be video model")
	}
}

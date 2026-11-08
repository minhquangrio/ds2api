package shared

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
)

type ModelsHandler struct {
	Store ConfigReader
}

// providerAvailability is asserted, not part of ConfigReader, so existing
// handler stubs keep compiling.
type providerAvailability interface {
	MuseEnabled() bool
}

func (h *ModelsHandler) ListModels(w http.ResponseWriter, _ *http.Request) {
	if reader, ok := h.Store.(providerAvailability); ok && !reader.MuseEnabled() {
		WriteJSON(w, http.StatusOK, config.OpenAIModelsResponseExcludingOwners("muse"))
		return
	}
	WriteJSON(w, http.StatusOK, config.OpenAIModelsResponse())
}

func (h *ModelsHandler) GetModel(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimSpace(chi.URLParam(r, "model_id"))
	model, ok := config.OpenAIModelByID(h.Store, modelID)
	if !ok {
		WriteOpenAIError(w, http.StatusNotFound, "Model not found.")
		return
	}
	WriteJSON(w, http.StatusOK, model)
}

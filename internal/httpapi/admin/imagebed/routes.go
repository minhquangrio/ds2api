package imagebed

import (
	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/image-bed/config", h.getConfig)
	r.Put("/image-bed/config", h.saveConfig)
	r.Delete("/image-bed/config", h.deleteConfig)
	r.Post("/image-bed/validate", h.validateConfig)
	r.Post("/image-bed/upload", h.uploadImage)
	r.Get("/image-bed/history", h.getHistory)
	r.Delete("/image-bed/history/{id}", h.deleteHistoryItem)
	r.Delete("/image-bed/history", h.clearHistory)
}

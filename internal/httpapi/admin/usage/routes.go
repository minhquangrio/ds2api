package usage

import "github.com/go-chi/chi/v5"

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/usage", h.getUsage)
	r.Post("/usage/backfill", h.postBackfill)
}

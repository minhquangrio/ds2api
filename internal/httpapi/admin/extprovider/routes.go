package extprovider

import (
	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/providers", h.listProviders)
	r.Post("/providers", h.createProvider)
	r.Get("/providers/{id}", h.getProvider)
	r.Put("/providers/{id}", h.updateProvider)
	r.Delete("/providers/{id}", h.deleteProvider)
	r.Post("/providers/{id}/sync", h.syncProvider)
	r.Post("/providers/{id}/inspect", h.inspectProvider)
}

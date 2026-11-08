package images

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the OpenAI-compatible images surface, including the
// bare-root aliases used by clients configured with the plain DS2API base URL.
func RegisterRoutes(r chi.Router, h *Handler) {
	if h.Ownership == nil {
		h.Ownership = NewMediaOwnership(0)
	}
	r.Post("/v1/images/generations", h.Generations)
	r.Post("/v1/images/edits", h.Edits)
	r.Get("/v1/images/media/{media_id}", h.MediaBytes)
	r.Post("/images/generations", h.Generations)
	r.Post("/images/edits", h.Edits)
	r.Get("/images/media/{media_id}", h.MediaBytes)
}

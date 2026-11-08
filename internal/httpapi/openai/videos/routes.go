package videos

import "github.com/go-chi/chi/v5"

// RegisterRoutes mounts the video surface plus the bare-root aliases used by
// clients configured with the plain DS2API base URL.
func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/v1/videos", h.Create)
	r.Get("/v1/videos", h.List)
	r.Get("/v1/videos/{video_id}", h.Retrieve)
	r.Get("/v1/videos/{video_id}/content", h.Content)
	r.Post("/videos", h.Create)
	r.Get("/videos", h.List)
	r.Get("/videos/{video_id}", h.Retrieve)
	r.Get("/videos/{video_id}/content", h.Content)
}

package codex

import (
	"github.com/go-chi/chi/v5"
)

func RegisterRoutes(r chi.Router, h *Handler) {
	r.Post("/codex/login/start", h.startLogin)
	r.Post("/codex/login/poll", h.pollLogin)
	r.Post("/codex/login/complete", h.completeLogin)
	r.Post("/codex/login/cancel", h.cancelLogin)

	r.Get("/codex/accounts", h.listAccounts)
	r.Post("/codex/accounts/refresh", h.refreshAccount)
	r.Post("/codex/accounts/test", h.testAccount)
	r.Delete("/codex/accounts", h.deleteAccount)
}

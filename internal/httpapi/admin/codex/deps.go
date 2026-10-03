package codex

import (
	adminshared "ds2api/internal/httpapi/admin/shared"
)

type Handler struct {
	Store adminshared.ConfigStore
	Pool  adminshared.PoolController
}

var writeJSON = adminshared.WriteJSON

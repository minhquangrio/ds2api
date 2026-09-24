package usage

import (
	"ds2api/internal/chathistory"
	adminshared "ds2api/internal/httpapi/admin/shared"
	"ds2api/internal/usageledger"
)

type Handler struct {
	Store       adminshared.ConfigStore
	Pool        adminshared.PoolController
	DS          adminshared.DeepSeekCaller
	OpenAI      adminshared.OpenAIChatCaller
	ChatHistory *chathistory.Store
	UsageLedger *usageledger.Store
}

var writeJSON = adminshared.WriteJSON

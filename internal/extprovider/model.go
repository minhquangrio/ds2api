package extprovider

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type ProviderModel struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name,omitempty"`
	ContextWindow      int      `json:"context_window,omitempty"`
	Capabilities       []string `json:"capabilities,omitempty"`        // "tools", "reasoning", "vision"
	CapabilitiesSource string   `json:"capabilities_source,omitempty"` // "inspected", "last_successful", "catalog"
	LastInspectAt      int64    `json:"last_inspect_at,omitempty"`
	InspectStatus      string   `json:"inspect_status,omitempty"` // "ready", "incompatible", "busy", "auth_error", "unavailable"
	InspectError       string   `json:"inspect_error,omitempty"`
}

type InspectionLog struct {
	ID        string `json:"id"`
	ModelID   string `json:"model_id"`
	Status    string `json:"status"`
	Details   string `json:"details,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

type Provider struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	BaseURL           string          `json:"base_url"`
	Token             string          `json:"token,omitempty"`
	ModelSource       string          `json:"model_source,omitempty"` // "discovered", "manual"
	Models            []ProviderModel `json:"models"`
	ConnectionStatus  string          `json:"connection_status"` // "connected", "error", "unknown"
	LastSyncAt        int64           `json:"last_sync_at,omitempty"`
	InspectionHistory []InspectionLog `json:"inspection_history,omitempty"`
}

type ProviderResponse struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	BaseURL           string          `json:"base_url"`
	HasToken          bool            `json:"has_token"`
	TokenMask         string          `json:"token_mask"`
	ModelSource       string          `json:"model_source,omitempty"`
	Models            []ProviderModel `json:"models"`
	ConnectionStatus  string          `json:"connection_status"`
	LastSyncAt        int64           `json:"last_sync_at,omitempty"`
	InspectionHistory []InspectionLog `json:"inspection_history,omitempty"`
}

func GenerateProviderID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("p_%d", time.Now().Unix())
	}
	return "p_" + hex.EncodeToString(b)
}

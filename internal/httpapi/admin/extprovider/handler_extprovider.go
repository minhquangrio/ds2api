package extprovider

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/extprovider"
	adminshared "ds2api/internal/httpapi/admin/shared"
)

type Handler struct {
	Store adminshared.ConfigStore
}

func toResponse(p extprovider.Provider) extprovider.ProviderResponse {
	if p.Models == nil {
		p.Models = []extprovider.ProviderModel{}
	}
	if p.InspectionHistory == nil {
		p.InspectionHistory = []extprovider.InspectionLog{}
	}
	return extprovider.ProviderResponse{
		ID:                p.ID,
		Name:              p.Name,
		BaseURL:           p.BaseURL,
		HasToken:          strings.TrimSpace(p.Token) != "",
		TokenMask:         adminshared.MaskSecretPreview(p.Token),
		ModelSource:       p.ModelSource,
		Models:            p.Models,
		ConnectionStatus:  p.ConnectionStatus,
		LastSyncAt:        p.LastSyncAt,
		InspectionHistory: p.InspectionHistory,
	}
}

func (h *Handler) listProviders(w http.ResponseWriter, _ *http.Request) {
	list, err := extprovider.ListProviders()
	if err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	items := make([]extprovider.ProviderResponse, 0, len(list))
	for _, p := range list {
		items = append(items, toResponse(p))
	}

	adminshared.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

func (h *Handler) createProvider(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string                      `json:"name"`
		BaseURL     string                      `json:"base_url"`
		Token       string                      `json:"token"`
		ModelSource string                      `json:"model_source"`
		Models      []extprovider.ProviderModel `json:"models"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	baseURL := extprovider.NormalizeBaseURL(input.BaseURL)
	if baseURL == "" {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "valid base_url is required"})
		return
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = baseURL
	}

	p := extprovider.Provider{
		ID:               extprovider.GenerateProviderID(),
		Name:             name,
		BaseURL:          baseURL,
		Token:            strings.TrimSpace(input.Token),
		ModelSource:      input.ModelSource,
		Models:           input.Models,
		ConnectionStatus: "unknown",
	}
	if p.Models == nil {
		p.Models = []extprovider.ProviderModel{}
	}

	// If not explicit manual models, try discovery
	if p.ModelSource != "manual" && len(p.Models) == 0 {
		models, err := extprovider.DiscoverModels(r.Context(), p.BaseURL, p.Token)
		if err == nil {
			p.Models = models
			p.ConnectionStatus = "connected"
		} else {
			p.ConnectionStatus = "error"
		}
	}

	if err := extprovider.SaveProvider(p); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, toResponse(p))
}

func (h *Handler) getProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := extprovider.GetProvider(id)
	if err != nil {
		adminshared.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "provider not found"})
		return
	}
	adminshared.WriteJSON(w, http.StatusOK, toResponse(*p))
}

func (h *Handler) updateProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := extprovider.GetProvider(id)
	if err != nil {
		adminshared.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "provider not found"})
		return
	}

	var input struct {
		Name        string                      `json:"name"`
		BaseURL     string                      `json:"base_url"`
		Token       string                      `json:"token"`
		ModelSource string                      `json:"model_source"`
		Models      []extprovider.ProviderModel `json:"models"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	if strings.TrimSpace(input.Name) != "" {
		existing.Name = strings.TrimSpace(input.Name)
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		existing.BaseURL = extprovider.NormalizeBaseURL(input.BaseURL)
	}
	if strings.TrimSpace(input.Token) != "" {
		existing.Token = strings.TrimSpace(input.Token)
	}
	if input.ModelSource != "" {
		existing.ModelSource = input.ModelSource
	}
	if input.Models != nil {
		existing.Models = input.Models
	}

	if err := extprovider.SaveProvider(*existing); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, toResponse(*existing))
}

func (h *Handler) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := extprovider.DeleteProvider(id); err != nil {
		adminshared.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "provider not found"})
		return
	}
	adminshared.WriteJSON(w, http.StatusOK, map[string]any{"message": "provider deleted"})
}

func (h *Handler) syncProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := extprovider.GetProvider(id)
	if err != nil {
		adminshared.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "provider not found"})
		return
	}

	models, err := extprovider.DiscoverModels(r.Context(), p.BaseURL, p.Token)
	if err != nil {
		p.ConnectionStatus = "error"
		_ = extprovider.SaveProvider(*p)
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{
			"error":    err.Error(),
			"provider": toResponse(*p),
		})
		return
	}

	// Preserve existing model inspection data if present
	existingMap := make(map[string]extprovider.ProviderModel)
	for _, m := range p.Models {
		existingMap[m.ID] = m
	}
	for i, m := range models {
		if prev, ok := existingMap[m.ID]; ok {
			models[i].Capabilities = prev.Capabilities
			models[i].CapabilitiesSource = prev.CapabilitiesSource
			models[i].InspectStatus = prev.InspectStatus
			models[i].InspectError = prev.InspectError
			models[i].LastInspectAt = prev.LastInspectAt
		}
	}

	p.Models = models
	p.ConnectionStatus = "connected"
	if err := extprovider.SaveProvider(*p); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, toResponse(*p))
}

func (h *Handler) inspectProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := extprovider.GetProvider(id)
	if err != nil {
		adminshared.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "provider not found"})
		return
	}

	if err := extprovider.InspectProvider(r.Context(), p); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	if err := extprovider.SaveProvider(*p); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, toResponse(*p))
}

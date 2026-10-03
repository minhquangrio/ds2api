package imagebed

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	adminshared "ds2api/internal/httpapi/admin/shared"
	"ds2api/internal/imagebed"
)

type Handler struct {
	Store adminshared.ConfigStore
}

func (h *Handler) getConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, err := imagebed.LoadConfig()
	if err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, imagebed.ConfigResponse{
		HasToken:   cfg.Token != "",
		TokenMask:  adminshared.MaskSecretPreview(cfg.Token),
		Owner:      cfg.Owner,
		Repository: cfg.Repository,
		PathPrefix: cfg.PathPrefix,
		Branch:     cfg.Branch,
	})
}

func (h *Handler) saveConfig(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token      string `json:"token"`
		Owner      string `json:"owner"`
		Repository string `json:"repository"`
		PathPrefix string `json:"path_prefix"`
		Branch     string `json:"branch"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	existing, _ := imagebed.LoadConfig()

	token := strings.TrimSpace(input.Token)
	if token == "" && existing.Token != "" {
		token = existing.Token
	}

	// The console has no branch field, so an omitted branch means "keep what is
	// configured". An empty stored value resolves to the repository's own
	// default branch at request time.
	branch := strings.TrimSpace(input.Branch)
	if branch == "" {
		branch = existing.Branch
	}

	cfg := imagebed.Config{
		Token:      token,
		Owner:      strings.TrimSpace(input.Owner),
		Repository: strings.TrimSpace(input.Repository),
		PathPrefix: strings.TrimSpace(input.PathPrefix),
		Branch:     branch,
	}

	if cfg.Owner == "" || cfg.Repository == "" {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "owner and repository are required"})
		return
	}

	if err := imagebed.SaveConfig(cfg); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, map[string]any{
		"message": "configuration saved",
		"config": imagebed.ConfigResponse{
			HasToken:   cfg.Token != "",
			TokenMask:  adminshared.MaskSecretPreview(cfg.Token),
			Owner:      cfg.Owner,
			Repository: cfg.Repository,
			PathPrefix: cfg.PathPrefix,
			Branch:     cfg.Branch,
		},
	})
}

func (h *Handler) deleteConfig(w http.ResponseWriter, _ *http.Request) {
	if err := imagebed.ClearConfig(); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	adminshared.WriteJSON(w, http.StatusOK, map[string]any{"message": "configuration cleared"})
}

func (h *Handler) validateConfig(w http.ResponseWriter, r *http.Request) {
	var input imagebed.Config
	_ = json.NewDecoder(r.Body).Decode(&input)

	stored, _ := imagebed.LoadConfig()
	if input.Token == "" {
		input.Token = stored.Token
	}
	if input.Owner == "" {
		input.Owner = stored.Owner
	}
	if input.Repository == "" {
		input.Repository = stored.Repository
	}
	if input.Branch == "" {
		input.Branch = stored.Branch
	}
	if input.PathPrefix == "" {
		input.PathPrefix = stored.PathPrefix
	}

	repo, err := imagebed.EnsureRepository(r.Context(), input)
	if err != nil {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, map[string]any{
		"valid":          true,
		"default_branch": repo.DefaultBranch,
	})
}

func (h *Handler) uploadImage(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DataURL string `json:"data_url"`
		Name    string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	if strings.TrimSpace(input.DataURL) == "" {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "data_url is required"})
		return
	}

	cfg, err := imagebed.LoadConfig()
	if err != nil || cfg.Token == "" {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "image bed is not configured"})
		return
	}

	item, err := imagebed.UploadImage(r.Context(), cfg, input.DataURL, input.Name)
	if err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) getHistory(w http.ResponseWriter, _ *http.Request) {
	items, err := imagebed.LoadHistory()
	if err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	adminshared.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

func (h *Handler) deleteHistoryItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		adminshared.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "id parameter required"})
		return
	}

	cfg, _ := imagebed.LoadConfig()

	items, err := imagebed.LoadHistory()
	if err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	var target *imagebed.HistoryItem
	for _, it := range items {
		if it.ID == id {
			target = &it
			break
		}
	}

	if target == nil {
		adminshared.WriteJSON(w, http.StatusNotFound, map[string]any{"error": "item not found"})
		return
	}

	// Delete from GitHub first if configured
	if cfg.Token != "" && cfg.Owner != "" && cfg.Repository != "" {
		_ = imagebed.DeleteRemoteFile(r.Context(), cfg, target.Path, target.SHA)
	}

	if _, err := imagebed.DeleteHistoryItem(id); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	adminshared.WriteJSON(w, http.StatusOK, map[string]any{"message": "item deleted"})
}

func (h *Handler) clearHistory(w http.ResponseWriter, _ *http.Request) {
	if err := imagebed.ClearHistory(); err != nil {
		adminshared.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	adminshared.WriteJSON(w, http.StatusOK, map[string]any{"message": "history cleared"})
}

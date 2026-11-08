package videos

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
	"ds2api/internal/config"
	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/mediacompat"
	"ds2api/internal/muse"
	"ds2api/internal/promptcompat"
	"ds2api/internal/responsehistory"
	"ds2api/internal/usageledger"
)

// Handler serves the video surface for the muse provider. Video is
// asynchronous: create returns a task id immediately and the caller polls, so
// no HTTP request blocks for the length of a generation.
type Handler struct {
	Store       shared.ConfigReader
	Auth        shared.AuthResolver
	ChatHistory *chathistory.Store
	UsageLedger *usageledger.Store
}

// Create handles POST /v1/videos (JSON body or multipart with input_reference).
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	payload, references, err := h.readCreatePayload(w, r)
	if err != nil {
		writeDecodeError(w, err)
		return
	}
	requested, _ := payload["model"].(string)
	if strings.TrimSpace(requested) == "" {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "model is required")
		return
	}
	target, resolved := config.ResolveModelTarget(h.Store, strings.TrimSpace(requested))
	if !resolved || target.Provider != "muse" {
		shared.WriteOpenAIErrorWithCode(w, http.StatusBadRequest,
			"the videos API is served by the muse provider; configure a muse account or use a muse model",
			"unsupported_provider")
		return
	}
	r.Header.Set("X-Ds2-Target-Provider", "muse")

	a, err := h.Auth.Determine(r)
	if err != nil {
		status, detail := auth.MapAuthStatus(err)
		shared.WriteOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)

	if err := h.Auth.EnforceKeyModelQuota(r, h.UsageLedger, target.Canonical); err != nil {
		status, detail := auth.MapAuthStatus(err)
		shared.WriteOpenAIError(w, status, detail)
		return
	}

	req, err := mediacompat.NormalizeVideoRequest(h.Store, payload, references)
	if err != nil {
		writeNormalizeError(w, err)
		return
	}
	req.CallerID = a.CallerID

	session := h.startHistory(r, a, req)
	onSwitched := func(newAccountID string) {
		if session != nil {
			session.SetAccountID(newAccountID)
		}
	}

	task, err := muse.ExecuteVideoCreateWithRetry(r.Context(), a, h.Store, req, onSwitched)
	if err != nil {
		status, code := muse.MediaErrorDetail(err)
		if session != nil {
			session.Error(status, err.Error(), code, "", "")
		}
		shared.WriteOpenAIErrorWithCode(w, status, err.Error(), code)
		return
	}
	if session != nil {
		session.Success(http.StatusCreated, "", "video job "+task.ID+" accepted", "stop", map[string]any{"videos": 1})
	}
	shared.WriteJSON(w, http.StatusCreated, renderTask(task, req.ResponseModel))
}

// Retrieve handles GET /v1/videos/{video_id} for the calling key.
func (h *Handler) Retrieve(w http.ResponseWriter, r *http.Request) {
	a, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	defer h.Auth.Release(a)

	id := strings.TrimSpace(chi.URLParam(r, "video_id"))
	if id == "" {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "video id is required")
		return
	}
	task, err := muse.PollVideo(r.Context(), a, h.Store, id)
	if err != nil {
		status, code := muse.MediaErrorDetail(err)
		shared.WriteOpenAIErrorWithCode(w, status, err.Error(), code)
		return
	}
	shared.WriteJSON(w, http.StatusOK, renderTask(task, ""))
}

// Content handles GET /v1/videos/{video_id}/content and streams the finished
// bytes. Caller ownership is enforced by the worker through the caller id.
func (h *Handler) Content(w http.ResponseWriter, r *http.Request) {
	a, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	defer h.Auth.Release(a)

	id := strings.TrimSpace(chi.URLParam(r, "video_id"))
	if id == "" {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "video id is required")
		return
	}
	task, err := muse.PollVideo(r.Context(), a, h.Store, id)
	if err != nil {
		status, code := muse.MediaErrorDetail(err)
		shared.WriteOpenAIErrorWithCode(w, status, err.Error(), code)
		return
	}
	if strings.ToLower(task.Status) != "completed" {
		shared.WriteOpenAIErrorWithCode(w, http.StatusConflict, "video job is not completed ("+task.Status+")", "job_incomplete")
		return
	}
	data, contentType, err := muse.FetchVideoContent(r.Context(), a, h.Store, id)
	if err != nil {
		status, code := muse.MediaErrorDetail(err)
		shared.WriteOpenAIErrorWithCode(w, status, err.Error(), code)
		return
	}
	if contentType == "" {
		contentType = "video/mp4"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+id+"."+videoExtension(contentType)+"\"")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if _, writeErr := w.Write(data); writeErr != nil {
		return
	}
}

// List handles GET /v1/videos, returning only the calling key's jobs.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	defer h.Auth.Release(a)

	tasks, err := muse.ListVideos(r.Context(), a, h.Store)
	if err != nil {
		status, code := muse.MediaErrorDetail(err)
		shared.WriteOpenAIErrorWithCode(w, status, err.Error(), code)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"object": "list", "data": renderTasks(tasks)})
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (*auth.RequestAuth, bool) {
	a, err := h.Auth.DetermineForProvider(r, "muse")
	if err != nil {
		status, detail := auth.MapAuthStatus(err)
		shared.WriteOpenAIError(w, status, detail)
		return nil, false
	}
	return a, true
}

func (h *Handler) startHistory(r *http.Request, a *auth.RequestAuth, req mediacompat.VideoRequest) *responsehistory.Session {
	if h.ChatHistory == nil && h.UsageLedger == nil {
		return nil
	}
	return responsehistory.Start(responsehistory.StartParams{
		Store: h.ChatHistory, Ledger: h.UsageLedger, Request: r, Auth: a,
		Surface: req.Surface, Standard: historyStandard(req),
	})
}

func historyStandard(req mediacompat.VideoRequest) promptcompat.StandardRequest {
	return promptcompat.StandardRequest{
		Surface:         req.Surface,
		RequestedModel:  req.RequestedModel,
		ResolvedModel:   req.ResolvedModel,
		ResponseModel:   req.ResponseModel,
		FinalPrompt:     req.Prompt,
		PromptTokenText: req.Prompt,
		Messages:        []any{map[string]any{"role": "user", "content": req.Prompt}},
	}
}

package images

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

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

// Handler serves the OpenAI images surface. Provider routing is decided by the
// shared Go model resolver before any account is acquired, exactly like the
// chat and responses transports, so no protocol adapter owns business behaviour.
type Handler struct {
	Store       shared.ConfigReader
	Auth        shared.AuthResolver
	ChatHistory *chathistory.Store
	UsageLedger *usageledger.Store
	Ownership   *mediaOwnership
}

// Generations handles POST /v1/images/generations (text to image).
func (h *Handler) Generations(w http.ResponseWriter, r *http.Request) {
	if isMultipartRequest(r) {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "generations accepts a JSON body")
		return
	}
	h.handle(w, r, mediacompat.SurfaceImagesGeneration, false)
}

// Edits handles POST /v1/images/edits (reference image plus instruction).
// Multipart file upload and JSON data-URI references are both accepted.
func (h *Handler) Edits(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r, mediacompat.SurfaceImagesEdit, isMultipartRequest(r))
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request, surface string, multipart bool) {
	var (
		payload    map[string]any
		references []mediacompat.Reference
		err        error
	)
	if multipart {
		payload, references, err = parseFormBody(w, r)
	} else {
		payload, references, err = parseJSONBody(w, r)
	}
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
			"the images API is served by the muse provider; configure a muse account or use a muse model",
			"unsupported_provider")
		return
	}
	// Provider detection happens before auth so no DeepSeek login/PoW is used.
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

	req, err := mediacompat.NormalizeImageRequest(h.Store, payload, surface, references)
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

	result, err := muse.ExecuteImageWithRetry(r.Context(), a, h.Store, req, onSwitched)
	if err != nil {
		status, code := muse.MediaErrorDetail(err)
		if session != nil {
			session.Error(status, err.Error(), code, "", "")
		}
		shared.WriteOpenAIErrorWithCode(w, status, err.Error(), code)
		return
	}

	h.Ownership.register(a.CallerID, result)
	body := renderImagesResponse(req, result, h.publicBaseURL(r))
	if session != nil {
		session.Success(http.StatusOK, "", historyContent(req, result), "stop", usageForHistory(result))
	}
	shared.WriteJSON(w, http.StatusOK, body)
}

func (h *Handler) startHistory(r *http.Request, a *auth.RequestAuth, req mediacompat.ImageRequest) *responsehistory.Session {
	if h.ChatHistory == nil && h.UsageLedger == nil {
		return nil
	}
	return responsehistory.Start(responsehistory.StartParams{
		Store:    h.ChatHistory,
		Ledger:   h.UsageLedger,
		Request:  r,
		Auth:     a,
		Surface:  req.Surface,
		Standard: historyStandard(req),
	})
}

// historyStandard projects the media request onto the shared history record.
// Media never becomes an assistantturn.Turn: an image is not empty text.
func historyStandard(req mediacompat.ImageRequest) promptcompat.StandardRequest {
	return promptcompat.StandardRequest{
		Surface:         req.Surface,
		RequestedModel:  req.RequestedModel,
		ResolvedModel:   req.ResolvedModel,
		ResponseModel:   req.ResponseModel,
		FinalPrompt:     req.Prompt,
		PromptTokenText: req.Prompt,
		Stream:          false,
		Messages:        []any{map[string]any{"role": "user", "content": req.Prompt}},
	}
}

func historyContent(req mediacompat.ImageRequest, result mediacompat.MediaResult) string {
	return "generated " + strconv.Itoa(len(result.Items)) + " image(s) for: " + truncate(req.Prompt, 200)
}

func usageForHistory(result mediacompat.MediaResult) map[string]any {
	usage := map[string]any{"images": len(result.Items)}
	for key, value := range result.Usage {
		usage[key] = value
	}
	return usage
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func writeDecodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTooLarge):
		shared.WriteOpenAIError(w, http.StatusRequestEntityTooLarge, errTooLarge.Error())
	case errors.Is(err, errInvalidMultipart), errors.Is(err, errNotMultipart):
		shared.WriteOpenAIError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errUnsupportedField):
		shared.WriteOpenAIError(w, http.StatusBadRequest, errUnsupportedField.Error())
	default:
		shared.WriteOpenAIError(w, http.StatusBadRequest, errInvalidJSON.Error())
	}
}

func writeNormalizeError(w http.ResponseWriter, err error) {
	if errors.Is(err, mediacompat.ErrInvalidMediaRequest) {
		message := strings.TrimPrefix(err.Error(), mediacompat.ErrInvalidMediaRequest.Error()+": ")
		shared.WriteOpenAIError(w, http.StatusBadRequest, message)
		return
	}
	shared.WriteOpenAIError(w, http.StatusBadRequest, err.Error())
}

func isMultipartRequest(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/form-data")
}

func (h *Handler) publicBaseURL(r *http.Request) string {
	if base := mediacompat.PublicBaseURL(); base != "" {
		return base
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

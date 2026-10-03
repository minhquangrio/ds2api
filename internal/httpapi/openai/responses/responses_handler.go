package responses

import (
	"ds2api/internal/toolcall"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"ds2api/internal/assistantturn"
	"ds2api/internal/auth"
	"ds2api/internal/codex"
	"ds2api/internal/completionruntime"
	"ds2api/internal/config"
	dsprotocol "ds2api/internal/deepseek/protocol"
	openaifmt "ds2api/internal/format/openai"
	"ds2api/internal/promptcompat"
	"ds2api/internal/responsehistory"
	"ds2api/internal/sse"
	streamengine "ds2api/internal/stream"
)

func (h *Handler) GetResponseByID(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.DetermineCaller(r)
	if err != nil {
		writeOpenAIError(w, http.StatusUnauthorized, err.Error())
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "response_id"))
	if id == "" {
		writeOpenAIError(w, http.StatusBadRequest, "response_id is required.")
		return
	}
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	st := h.getResponseStore()
	item, ok := st.get(owner, id)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "Response not found.")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	// The body is read before auth so the requested model can select the target
	// provider, matching the chat/Claude/Gemini entry points. Without this the
	// resolver would default to DeepSeek and provider-specific models could
	// never be reached through this endpoint.
	r.Body = http.MaxBytesReader(w, r.Body, openAIGeneralMaxSize)
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			writeOpenAIError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeOpenAIError(w, http.StatusBadRequest, "invalid json")
		return
	}

	requestedModel, _ := req["model"].(string)
	target, targetResolved := config.ResolveModelTarget(h.Store, requestedModel)
	if targetResolved && target.Provider != "" {
		r.Header.Set("X-Ds2-Target-Provider", target.Provider)
	}

	a, err := h.Auth.Determine(r)
	if err != nil {
		status, detail := auth.MapAuthStatus(err)
		writeOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)
	r = r.WithContext(auth.WithAuth(r.Context(), a))
	owner := responseStoreOwner(a)
	if owner == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	originalModel, rerouted := promptcompat.MaybeAutoRouteVision(req, h.Store)
	if err := h.preprocessInlineFileInputs(r.Context(), a, req); err != nil {
		writeOpenAIInlineFileError(w, err)
		return
	}
	if err := h.preprocessInlineTextFilesForExpert(r.Context(), a, req); err != nil {
		writeOpenAIInlineFileError(w, err)
		return
	}
	if rerouted {
		promptcompat.StripImageBlocksFromRequest(req)
	}
	if !h.Auth.ToolsEnabledForRequest(r) {
		delete(req, "tools")
		delete(req, "tool_choice")
	}
	traceID := requestTraceID(r)
	stdReq, err := promptcompat.NormalizeOpenAIResponsesRequest(h.Store, req, traceID)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.Auth.EnforceKeyModelQuota(r, h.UsageLedger, stdReq.ResolvedModel); err != nil {
		status, detail := auth.MapAuthStatus(err)
		writeOpenAIError(w, status, detail)
		return
	}
	if rerouted && originalModel != "" {
		stdReq.RequestedModel = originalModel
		stdReq.ResponseModel = originalModel
	}
	stdReq, err = h.applyCurrentInputFile(r.Context(), a, stdReq)
	if err != nil {
		status, message := mapCurrentInputFileError(err)
		writeOpenAIError(w, status, message)
		return
	}

	responseID := "resp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	historySession := responsehistory.Start(responsehistory.StartParams{
		Store:    h.ChatHistory,
		Ledger:   h.UsageLedger,
		Request:  r,
		Auth:     a,
		Surface:  "openai.responses",
		Standard: stdReq,
	})

	if a.Provider == "codex" {
		client, err := codex.DefaultRuntime().GetClient(r.Context(), a.Account, h.Store)
		if err != nil {
			if historySession != nil {
				historySession.Error(http.StatusBadGateway, err.Error(), "upstream_error", "", "")
			}
			writeOpenAIError(w, http.StatusBadGateway, err.Error())
			return
		}

		if !stdReq.Stream {
			turn, err := codex.ExecuteTurn(r.Context(), client, stdReq)
			if err != nil {
				if historySession != nil {
					historySession.Error(http.StatusBadGateway, err.Error(), "upstream_error", "", "")
				}
				writeOpenAIError(w, http.StatusBadGateway, err.Error())
				return
			}
			if historySession != nil {
				historySession.SuccessTurn(http.StatusOK, turn, assistantturn.OpenAIResponsesUsage(turn))
			}
			responseObj := openaifmt.BuildResponseObjectWithToolCalls(responseID, stdReq.ResponseModel, turn.Prompt, turn.Thinking, turn.Text, turn.ToolCalls, stdReq.ToolsRaw)
			responseObj["usage"] = assistantturn.OpenAIResponsesUsage(turn)
			h.getResponseStore().put(owner, responseID, responseObj)
			writeJSON(w, http.StatusOK, responseObj)
			return
		}

		if err := codex.StreamResponsesAPI(r.Context(), client, stdReq, w); err != nil {
			config.Logger.Warn("[codex] responses stream error", "error", err)
			if historySession != nil {
				historySession.Error(http.StatusBadGateway, err.Error(), "upstream_error", "", "")
			}
			return
		}
		if historySession != nil {
			historySession.Success(http.StatusOK, "", "", "stop", nil)
		}
		return
	}

	if !stdReq.Stream {
		result, outErr := completionruntime.ExecuteNonStreamWithRetry(r.Context(), h.DS, a, stdReq, completionruntime.Options{
			RetryEnabled:        true,
			CurrentInputFile:    h.Store,
			ExpertPromptSegment: h.Store,
		})
		if outErr != nil {
			if historySession != nil {
				if completionruntime.IsDirectTokenAuthError(outErr) {
					historySession.Discard()
				} else {
					historySession.ErrorTurn(outErr.Status, outErr.Message, outErr.Code, result.Turn)
				}
			}
			writeOpenAIErrorWithCode(w, outErr.Status, outErr.Message, outErr.Code)
			return
		}
		if historySession != nil {
			historySession.SuccessTurn(http.StatusOK, result.Turn, assistantturn.OpenAIResponsesUsage(result.Turn))
		}
		responseObj := openaifmt.BuildResponseObjectWithToolCalls(responseID, stdReq.ResponseModel, result.Turn.Prompt, result.Turn.Thinking, result.Turn.Text, result.Turn.ToolCalls, stdReq.ToolsRaw)
		responseObj["usage"] = assistantturn.OpenAIResponsesUsage(result.Turn)
		h.getResponseStore().put(owner, responseID, responseObj)
		writeJSON(w, http.StatusOK, responseObj)
		return
	}

	start, outErr := completionruntime.StartCompletion(r.Context(), h.DS, a, stdReq, completionruntime.Options{
		CurrentInputFile:    h.Store,
		ExpertPromptSegment: h.Store,
	})
	if outErr != nil {
		if historySession != nil {
			if completionruntime.IsDirectTokenAuthError(outErr) {
				historySession.Discard()
			} else {
				historySession.Error(outErr.Status, outErr.Message, outErr.Code, "", "")
			}
		}
		writeOpenAIErrorWithCode(w, outErr.Status, outErr.Message, outErr.Code)
		return
	}

	streamReq := start.Request
	refFileTokens := streamReq.RefFileTokens
	h.handleResponsesStreamWithRetry(w, r, a, start.Response, start.Payload, start.Pow, owner, responseID, streamReq, streamReq.ResponseModel, streamReq.PromptTokenText, refFileTokens, streamReq.Thinking, streamReq.Search, streamReq.ToolNames, streamReq.ToolsRaw, streamReq.ToolChoice, traceID, historySession)
}

func (h *Handler) handleResponsesNonStream(w http.ResponseWriter, resp *http.Response, owner, responseID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if detail := completionruntime.TryDetectCaptchaFromBody(body); detail != "" {
			config.Logger.Warn("[openai_responses_nonstream] captcha challenge detected on initial response", "detail", detail)
		}
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	result := sse.CollectStream(resp, thinkingEnabled, true)

	turn := assistantturn.BuildTurnFromCollected(result, assistantturn.BuildOptions{
		Model:         model,
		Prompt:        finalPrompt,
		RefFileTokens: refFileTokens,
		SearchEnabled: searchEnabled,
		ToolNames:     toolNames,
		ToolsRaw:      toolsRaw,
		ToolChoice:    toolChoice,
	})
	logResponsesToolPolicyRejection(traceID, toolChoice, turn.ParsedToolCalls, "text")
	outcome := assistantturn.FinalizeTurn(turn, assistantturn.FinalizeOptions{})
	if outcome.ShouldFail {
		writeOpenAIErrorWithCode(w, outcome.Error.Status, outcome.Error.Message, outcome.Error.Code)
		return
	}

	responseObj := openaifmt.BuildResponseObjectWithToolCalls(responseID, model, finalPrompt, turn.Thinking, turn.Text, turn.ToolCalls, toolsRaw)
	responseObj["usage"] = assistantturn.OpenAIResponsesUsage(turn)
	h.getResponseStore().put(owner, responseID, responseObj)
	writeJSON(w, http.StatusOK, responseObj)
}

func (h *Handler) handleResponsesStream(w http.ResponseWriter, r *http.Request, resp *http.Response, owner, responseID, model, finalPrompt string, refFileTokens int, thinkingEnabled, searchEnabled bool, toolNames []string, toolsRaw any, toolChoice promptcompat.ToolChoicePolicy, traceID string) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if detail := completionruntime.TryDetectCaptchaFromBody(body); detail != "" {
			config.Logger.Warn("[openai_responses_stream] captcha challenge detected on initial response", "detail", detail)
		}
		writeOpenAIError(w, resp.StatusCode, strings.TrimSpace(string(body)))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	rc := http.NewResponseController(w)
	_, canFlush := w.(http.Flusher)

	initialType := "text"
	if thinkingEnabled {
		initialType = "thinking"
	}
	bufferToolContent := len(toolNames) > 0
	emitEarlyToolDeltas := h.toolcallFeatureMatchEnabled() && h.toolcallEarlyEmitHighConfidence()
	stripReferenceMarkers := stripReferenceMarkersEnabled()

	streamRuntime := newResponsesStreamRuntime(
		w,
		rc,
		canFlush,
		responseID,
		model,
		finalPrompt,
		thinkingEnabled,
		searchEnabled,
		stripReferenceMarkers,
		toolNames,
		toolsRaw,
		bufferToolContent,
		emitEarlyToolDeltas,
		toolChoice,
		traceID,
		func(obj map[string]any) {
			h.getResponseStore().put(owner, responseID, obj)
		},
		nil,
	)
	streamRuntime.refFileTokens = refFileTokens
	streamRuntime.sendCreated()

	streamengine.ConsumeSSE(streamengine.ConsumeConfig{
		Context:             r.Context(),
		Body:                resp.Body,
		ThinkingEnabled:     thinkingEnabled,
		InitialType:         initialType,
		KeepAliveInterval:   time.Duration(dsprotocol.KeepAliveTimeout) * time.Second,
		IdleTimeout:         time.Duration(dsprotocol.StreamIdleTimeout) * time.Second,
		MaxKeepAliveNoInput: dsprotocol.MaxKeepaliveCount,
	}, streamengine.ConsumeHooks{
		OnParsed: streamRuntime.onParsed,
		OnFinalize: func(reason streamengine.StopReason, _ error) {
			if string(reason) == "content_filter" {
				streamRuntime.finalize("content_filter", false)
				return
			}
			streamRuntime.finalize("stop", false)
		},
	})
}

func logResponsesToolPolicyRejection(traceID string, policy promptcompat.ToolChoicePolicy, parsed toolcall.ToolCallParseResult, channel string) {
	rejected := filteredRejectedToolNamesForLog(parsed.RejectedToolNames)
	if !parsed.RejectedByPolicy || len(rejected) == 0 {
		return
	}
	config.Logger.Warn(
		"[responses] rejected tool calls by policy",
		"trace_id", strings.TrimSpace(traceID),
		"channel", channel,
		"tool_choice_mode", policy.Mode,
		"rejected_tool_names", strings.Join(rejected, ","),
	)
}

func filteredRejectedToolNamesForLog(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		switch strings.ToLower(trimmed) {
		case "", "tool_name":
			continue
		default:
			out = append(out, trimmed)
		}
	}
	return out
}

func (h *Handler) CodexResponsesPassthrough(w http.ResponseWriter, r *http.Request) {
	a, err := h.Auth.DetermineForProvider(r, "codex")
	if err != nil {
		status, detail := auth.MapAuthStatus(err)
		writeOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)

	client, err := codex.DefaultRuntime().GetClient(r.Context(), a.Account, h.Store)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error())
		return
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, codex.CodexResponsesURL, r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	upstreamReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	if reqID := r.Header.Get("X-Request-Id"); reqID != "" {
		upstreamReq.Header.Set("X-Request-Id", reqID)
	}
	for k, vv := range r.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-codex-") {
			for _, v := range vv {
				upstreamReq.Header.Add(k, v)
			}
		}
	}

	resp, err := client.DoStream(upstreamReq)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			config.Logger.Warn("[codex] failed to close passthrough response body", "error", cerr)
		}
	}()

	// Only relay the headers a client can act on. Copying the whole upstream
	// response would also forward framing headers (Content-Length,
	// Content-Encoding, Transfer-Encoding) that no longer describe the body we
	// are about to write.
	for k, vv := range resp.Header {
		lower := strings.ToLower(k)
		if lower == "content-type" || lower == "x-request-id" || strings.HasPrefix(lower, "x-codex-") {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	}
	w.WriteHeader(resp.StatusCode)

	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = w.Write(buf[:n])
			if canFlush {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				config.Logger.Warn("[codex] passthrough stream interrupted", "error", rerr)
			}
			break
		}
	}
}

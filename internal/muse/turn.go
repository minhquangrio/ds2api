package muse

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ds2api/internal/assistantturn"
	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/promptcompat"
)

type workerCompletion struct {
	Text         string         `json:"text"`
	Reasoning    string         `json:"reasoning"`
	FinishReason string         `json:"finish_reason"`
	Usage        map[string]int `json:"usage"`
	Cookies      *string        `json:"cookies"`
}

func workerModel(req promptcompat.StandardRequest) string {
	model := req.ResolvedModel
	if model == "" {
		model = req.RequestedModel
	}
	if model == "" {
		model = ModelMuseSpark
	}
	return strings.TrimPrefix(cleanModelName(model), "muse/")
}

func workerPrompt(req promptcompat.StandardRequest) string {
	if req.FinalPrompt != "" {
		return req.FinalPrompt
	}
	return req.PromptTokenText
}

func buildWorkerTurn(req promptcompat.StandardRequest, result workerCompletion) assistantturn.Turn {
	thinking := ""
	if req.Thinking {
		thinking = result.Reasoning
	}
	turn := assistantturn.BuildTurnFromStreamSnapshot(assistantturn.StreamSnapshot{
		RawText: result.Text, VisibleText: result.Text,
		RawThinking: result.Reasoning, DetectionThinking: result.Reasoning,
		VisibleThinking: thinking, ContentFilter: result.FinishReason == "content_filter",
	}, assistantturn.BuildOptions{
		Model: req.ResponseModel, Prompt: workerPrompt(req),
		ToolNames: req.ToolNames, ToolsRaw: req.ToolsRaw, ToolChoice: req.ToolChoice,
	})
	// OpenAI completion_tokens already includes reasoning_tokens.
	if usage := result.Usage; usage["prompt_tokens"] > 0 || usage["completion_tokens"] > 0 {
		turn.Usage = assistantturn.Usage{
			InputTokens: usage["prompt_tokens"], OutputTokens: usage["completion_tokens"],
			ReasoningTokens: usage["reasoning_tokens"],
			TotalTokens:     usage["prompt_tokens"] + usage["completion_tokens"],
		}
	}
	return turn
}

func turnError(turn assistantturn.Turn) error {
	if turn.Error == nil {
		return nil
	}
	return fmt.Errorf("%s: %s", turn.Error.Code, turn.Error.Message)
}

// ErrorDetail preserves shared policy errors at every protocol boundary.
func ErrorDetail(turn assistantturn.Turn, err error) (int, string) {
	if errors.Is(err, ErrMuseInvalidModel) {
		return http.StatusBadRequest, "unsupported_model"
	}
	if turn.Error != nil && strings.HasPrefix(err.Error(), turn.Error.Code+":") {
		return turn.Error.Status, turn.Error.Code
	}
	if IsThrottledError(err) {
		return http.StatusTooManyRequests, "rate_limit_exceeded"
	}
	return http.StatusBadGateway, "upstream_error"
}

func retryEmpty(turn assistantturn.Turn, err error, attempt int) bool {
	return err != nil && turn.Error != nil && err.Error() == turnError(turn).Error() &&
		shared.EmptyOutputRetryEnabled() &&
		assistantturn.ShouldRetryEmptyOutput(turn, attempt, shared.EmptyOutputRetryMaxAttempts())
}

func emptyRetryRequest(req promptcompat.StandardRequest) promptcompat.StandardRequest {
	req.FinalPrompt = shared.AppendEmptyOutputRetrySuffix(workerPrompt(req))
	return req
}

package geminiweb

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"ds2api/internal/assistantturn"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

var (
	ErrGeminiThrottled              = errors.New("gemini upstream throttled (BardErrorInfo 1095)")
	ErrAllGeminiAccountsCoolingDown = errors.New("all upstream gemini accounts are cooling down")
)

func IsThrottledError(err error) bool { return errors.Is(err, ErrGeminiThrottled) }

// geminiCooldownDuration reads cooldown time from env, defaulting to 60s
// (5x safety margin compared to 12s experimental recovery time).
func geminiCooldownDuration() time.Duration {
	if raw := os.Getenv("DS2API_GEMINI_COOLDOWN_SECONDS"); raw != "" {
		if sec, err := strconv.Atoi(raw); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 60 * time.Second
}

// geminiRetryCeiling caps retry attempts by number of schedulable Gemini accounts,
// capped at 5 to prevent pathological retry loops. Returns at least 1.
func geminiRetryCeiling(store any) int {
	const ceiling = 5
	snap, ok := store.(StoreSnapshotter)
	if !ok {
		return 3
	}
	n := 0
	for _, acc := range snap.Snapshot().Accounts {
		if acc.IsGemini() && acc.IsSchedulable() {
			n++
		}
	}
	if n < 1 {
		return 1
	}
	if n > ceiling {
		return ceiling
	}
	return n
}

// switchAfterThrottle cools down the current account, evicts its cached client,
// and switches to the next schedulable account.
// Returns false if no switch is possible (exhausted pool, pinned target, or direct token).
func switchAfterThrottle(ctx context.Context, a *auth.RequestAuth, attempt int, op string) bool {
	config.Logger.Warn("[gemini_retry] upstream 1095 throttle detected",
		"op", op, "account", a.AccountID, "attempt", attempt+1)
	a.CoolDown(geminiCooldownDuration())
	DefaultRuntime().EvictClient(a.AccountID)
	return a.SwitchAccount(ctx)
}

// ExecuteTurnWithRetry executes a non-streaming turn with automatic account switching when throttled.
func ExecuteTurnWithRetry(
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	stdReq promptcompat.StandardRequest,
	onAccountSwitched func(newAccountID string),
) (assistantturn.Turn, error) {
	maxAttempts := geminiRetryCeiling(store)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
		if err != nil {
			return assistantturn.Turn{}, err
		}

		turn, execErr := ExecuteTurn(ctx, client, stdReq)
		if execErr == nil {
			return turn, nil
		}
		if !IsThrottledError(execErr) || !a.UseConfigToken || a.TargetAccount != "" {
			return assistantturn.Turn{}, execErr
		}
		if !switchAfterThrottle(ctx, a, attempt, "turn") {
			return assistantturn.Turn{}, ErrAllGeminiAccountsCoolingDown
		}
		if onAccountSwitched != nil {
			onAccountSwitched(a.AccountID)
		}
	}
	return assistantturn.Turn{}, ErrAllGeminiAccountsCoolingDown
}

// StreamRenderer renders a Gemini stream into a protocol-specific SSE output.
type StreamRenderer func(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error)

// ExecuteStreamWithRetry executes a stream with automatic account switching when throttled before first byte.
func ExecuteStreamWithRetry(
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	stdReq promptcompat.StandardRequest,
	w http.ResponseWriter,
	render StreamRenderer,
	onAccountSwitched func(newAccountID string),
) (string, string, error) {
	maxAttempts := geminiRetryCeiling(store)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
		if err != nil {
			return "", "", err
		}

		thinking, text, streamErr := render(ctx, client, stdReq, w)
		if streamErr == nil {
			return thinking, text, nil
		}
		if !IsThrottledError(streamErr) || !a.UseConfigToken || a.TargetAccount != "" {
			return thinking, text, streamErr
		}
		if !switchAfterThrottle(ctx, a, attempt, "stream") {
			return "", "", ErrAllGeminiAccountsCoolingDown
		}
		if onAccountSwitched != nil {
			onAccountSwitched(a.AccountID)
		}
	}
	return "", "", ErrAllGeminiAccountsCoolingDown
}

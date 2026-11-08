package muse

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

func museCooldownDuration() time.Duration {
	if raw := os.Getenv("DS2API_MUSE_COOLDOWN_SECONDS"); raw != "" {
		if sec, err := strconv.Atoi(raw); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 60 * time.Second
}

func museRetryCeiling(store any) int {
	const ceiling = 5
	snap, ok := store.(StoreSnapshotter)
	if !ok {
		return 3
	}
	n := 0
	for _, acc := range snap.Snapshot().Accounts {
		if acc.IsMuse() && acc.IsSchedulable() {
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

func switchAfterThrottle(ctx context.Context, a *auth.RequestAuth, attempt int, op string) bool {
	config.Logger.Warn("[muse_retry] upstream throttle detected",
		"op", op, "account", a.AccountID, "attempt", attempt+1)
	a.CoolDown(museCooldownDuration())
	DefaultRuntime().EvictClient(a.AccountID)
	return a.SwitchAccount(ctx)
}

// ExecuteTurnWithRetry executes a non-streaming turn with automatic account
// switching when throttled. The rotation rule itself lives in
// executeWithAccountRotation so text and media transports stay identical.
func ExecuteTurnWithRetry(
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	stdReq promptcompat.StandardRequest,
	onAccountSwitched func(newAccountID string),
) (assistantturn.Turn, error) {
	return executeWithAccountRotation(ctx, a, store, "turn", func(client *Client) (assistantturn.Turn, error) {
		return ExecuteTurn(ctx, client, stdReq)
	}, onAccountSwitched)
}

type commitTrackingWriter struct {
	http.ResponseWriter
	committed bool
}

func (c *commitTrackingWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		c.committed = true
	}
	return c.ResponseWriter.Write(p)
}

func (c *commitTrackingWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		c.committed = true
		f.Flush()
	}
}

func (c *commitTrackingWriter) Header() http.Header {
	return c.ResponseWriter.Header()
}

func (c *commitTrackingWriter) WriteHeader(statusCode int) {
	c.committed = true
	c.ResponseWriter.WriteHeader(statusCode)
}

// StreamRenderer renders a Muse stream into protocol-specific SSE output.
type StreamRenderer func(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error)

type TurnStreamRenderer func(context.Context, *Client, promptcompat.StandardRequest, http.ResponseWriter) (assistantturn.Turn, error)

type committedStreamError struct{ error }

func (e *committedStreamError) Unwrap() error { return e.error }

func StreamCommitted(err error) bool {
	var committed *committedStreamError
	return errors.As(err, &committed)
}

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
	turn, err := ExecuteStreamTurnWithRetry(ctx, a, store, stdReq, w, func(ctx context.Context, c *Client, req promptcompat.StandardRequest, w http.ResponseWriter) (assistantturn.Turn, error) {
		thinking, text, err := render(ctx, c, req, w)
		return assistantturn.Turn{Thinking: thinking, Text: text}, err
	}, onAccountSwitched)
	return turn.Thinking, turn.Text, err
}

// ExecuteStreamTurnWithRetry is shared by all interface protocols.
func ExecuteStreamTurnWithRetry(ctx context.Context, a *auth.RequestAuth, store any, stdReq promptcompat.StandardRequest, w http.ResponseWriter, render TurnStreamRenderer, onAccountSwitched func(string)) (assistantturn.Turn, error) {
	maxAttempts := museRetryCeiling(store)
	cw := &commitTrackingWriter{ResponseWriter: w}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
		if err != nil {
			return assistantturn.Turn{}, err
		}

		turn, streamErr := render(ctx, client, stdReq, cw)
		if streamErr == nil {
			return turn, nil
		}
		if cw.committed {
			return turn, &committedStreamError{streamErr}
		}
		if !IsThrottledError(streamErr) || !a.UseConfigToken || a.TargetAccount != "" {
			return turn, streamErr
		}
		if attempt+1 >= maxAttempts {
			a.CoolDown(museCooldownDuration())
			return turn, ErrAllMuseAccountsCoolingDown
		}
		if !switchAfterThrottle(ctx, a, attempt, "stream") {
			return turn, ErrAllMuseAccountsCoolingDown
		}
		if onAccountSwitched != nil {
			onAccountSwitched(a.AccountID)
		}
	}
	return assistantturn.Turn{}, ErrAllMuseAccountsCoolingDown
}

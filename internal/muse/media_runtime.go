package muse

import (
	"context"
	"errors"
	"net/http"

	"ds2api/internal/auth"
	"ds2api/internal/mediacompat"
)

// executeWithAccountRotation is the single Muse rotation rule shared by every
// transport: acquire a client, run one operation, and switch account only on an
// upstream throttle while nothing has been sent to the caller yet. Text turns,
// image calls and video submits all go through here so the cooldown, ceiling and
// caller allowlist behaviour can never drift between media types.
func executeWithAccountRotation[T any](
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	op string,
	run func(*Client) (T, error),
	onAccountSwitched func(newAccountID string),
) (T, error) {
	var zero T
	maxAttempts := museRetryCeiling(store)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
		if err != nil {
			return zero, err
		}
		value, execErr := run(client)
		if execErr == nil {
			return value, nil
		}
		if !IsThrottledError(execErr) || !a.UseConfigToken || a.TargetAccount != "" {
			return value, execErr
		}
		if attempt+1 >= maxAttempts {
			a.CoolDown(museCooldownDuration())
			return value, ErrAllMuseAccountsCoolingDown
		}
		if !switchAfterThrottle(ctx, a, attempt, op) {
			return value, ErrAllMuseAccountsCoolingDown
		}
		if onAccountSwitched != nil {
			onAccountSwitched(a.AccountID)
		}
	}
	return zero, ErrAllMuseAccountsCoolingDown
}

// ExecuteImageWithRetry runs one image generation or edit. Media generation is
// never retried for an empty or partial result (unlike text): a submitted job
// may already be billed upstream, so only a pre-send throttle rotates account.
func ExecuteImageWithRetry(
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	req mediacompat.ImageRequest,
	onAccountSwitched func(newAccountID string),
) (mediacompat.MediaResult, error) {
	return executeWithAccountRotation(ctx, a, store, "image", func(client *Client) (mediacompat.MediaResult, error) {
		return client.ExecuteImage(ctx, req)
	}, onAccountSwitched)
}

// ExecuteVideoCreateWithRetry submits one asynchronous video job.
func ExecuteVideoCreateWithRetry(
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	req mediacompat.VideoRequest,
	onAccountSwitched func(newAccountID string),
) (mediacompat.VideoTask, error) {
	return executeWithAccountRotation(ctx, a, store, "video", func(client *Client) (mediacompat.VideoTask, error) {
		return client.CreateVideo(ctx, req)
	}, onAccountSwitched)
}

// PollVideo reads a task with the caller's own identity; no rotation applies.
func PollVideo(ctx context.Context, a *auth.RequestAuth, store any, taskID string) (mediacompat.VideoTask, error) {
	client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
	if err != nil {
		return mediacompat.VideoTask{}, err
	}
	return client.PollVideo(ctx, taskID, a.CallerID)
}

// FetchVideoContent returns the finished bytes for a task owned by this caller.
func FetchVideoContent(ctx context.Context, a *auth.RequestAuth, store any, taskID string) ([]byte, string, error) {
	client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
	if err != nil {
		return nil, "", err
	}
	return client.FetchVideoContent(ctx, taskID, a.CallerID)
}

// ListVideos reads the caller's own video jobs from the worker.
func ListVideos(ctx context.Context, a *auth.RequestAuth, store any) ([]mediacompat.VideoTask, error) {
	client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
	if err != nil {
		return nil, err
	}
	return client.ListVideos(ctx, a.CallerID)
}

// FetchMediaBytes reads one stored image blob for url responses.
func FetchMediaBytes(ctx context.Context, a *auth.RequestAuth, store any, mediaID string) ([]byte, string, error) {
	client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
	if err != nil {
		return nil, "", err
	}
	return client.FetchMediaBytes(ctx, mediaID)
}

// MediaErrorDetail maps a media failure to an HTTP status and stable code,
// mirroring ErrorDetail for text turns without dragging the turn model in.
func MediaErrorDetail(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, ErrMuseInvalidModel):
		return http.StatusBadRequest, "unsupported_model"
	case errors.Is(err, ErrMuseJobNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, ErrAllMuseAccountsCoolingDown):
		return http.StatusTooManyRequests, "all_muse_accounts_cooling_down"
	case IsThrottledError(err):
		return http.StatusTooManyRequests, "rate_limit_exceeded"
	case IsAuthError(err):
		return http.StatusUnauthorized, "session_expired"
	case errors.Is(err, ErrMuseWorkerUnavailable):
		return http.StatusBadGateway, "worker_unavailable"
	default:
		return http.StatusBadGateway, "upstream_error"
	}
}

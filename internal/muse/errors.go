package muse

import (
	"errors"
	"strings"
)

var (
	ErrMuseWorkerUnavailable      = errors.New("muse worker service is unavailable")
	ErrMuseThrottled              = errors.New("upstream muse account throttled")
	ErrAllMuseAccountsCoolingDown = errors.New("all upstream muse accounts are cooling down")
	ErrMuseUnauthorized           = errors.New("muse authentication failed (check session cookies)")
	ErrMuseInvalidModel           = errors.New("unsupported muse model for operation")
)

// IsThrottledError reports whether the error represents upstream rate limiting or throttling.
func IsThrottledError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrMuseThrottled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "throttled") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "cooling down")
}

// IsAuthError reports whether the error represents an expired or invalid cookie session.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrMuseUnauthorized) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "session_expired") ||
		strings.Contains(msg, "missing muse session cookies") ||
		strings.Contains(msg, "invalid cookie")
}

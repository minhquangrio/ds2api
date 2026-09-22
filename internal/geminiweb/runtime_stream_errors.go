package geminiweb

import (
	"errors"
	"fmt"

	"ds2api/internal/config"
)

func upstreamEmptyError(accountID string, reader *StreamReader) error {
	var reason string
	if reader != nil {
		reason = reader.BlockReason()
	}
	config.Logger.Warn("[geminiweb] gemini upstream returned empty output", "account", accountID, "reason", reason)
	if reason != "" {
		return fmt.Errorf("gemini upstream returned empty output: %s (check proxy/cookies)", reason)
	}
	return errors.New("gemini upstream returned empty output (check proxy/cookies)")
}

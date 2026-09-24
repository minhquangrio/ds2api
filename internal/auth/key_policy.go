package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrModelNotAllowed = errors.New("requested model is not allowed for this API key")
	ErrQuotaExceeded   = errors.New("api key token quota exceeded")
)

type CallerTokenReader interface {
	CallerTotalTokens(callerID string) int64
}

func (r *Resolver) EnforceKeyModelQuota(req *http.Request, ledger CallerTokenReader, canonicalModel string) error {
	callerKey := extractCallerToken(req)
	if callerKey == "" {
		return nil
	}
	if r == nil || r.Store == nil || !r.Store.HasAPIKey(callerKey) {
		return nil
	}
	policy := r.Store.APIKeyPolicy(callerKey)

	if len(policy.Models) > 0 {
		canonical := strings.ToLower(strings.TrimSpace(canonicalModel))
		if _, ok := policy.Models[canonical]; !ok {
			return ErrModelNotAllowed
		}
	}

	if policy.QuotaTokens > 0 && ledger != nil {
		callerID := callerTokenID(callerKey)
		consumed := ledger.CallerTotalTokens(callerID)
		if consumed >= policy.QuotaTokens {
			return fmt.Errorf("%w: limit %d tokens, consumed %d tokens", ErrQuotaExceeded, policy.QuotaTokens, consumed)
		}
	}

	return nil
}

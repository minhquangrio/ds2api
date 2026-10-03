package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"ds2api/internal/config"
)

// CodexTokenRefresher performs the refresh. The account's proxy (when set) is
// passed through so a proxied deployment can reach auth.openai.com.
type CodexTokenRefresher func(ctx context.Context, refreshToken string, proxy *config.Proxy) (accessToken, newRefreshToken, idToken string, expiresIn int64, email, accID, planType string, err error)

// codexRefreshCall is the shared result of one in-flight refresh. Waiters read
// the same result instead of racing a second refresh, and — importantly — they
// apply the returned credentials to their own request-local account copy.
// Returning nil without doing so would leave a waiter using the expired token.
type codexRefreshCall struct {
	done         chan struct{}
	accessToken  string
	refreshToken string
	idToken      string
	expiresIn    int64
	email        string
	accID        string
	planType     string
	err          error
}

var (
	codexRefreshMu      sync.Mutex
	codexInFlight       = make(map[string]*codexRefreshCall)
	codexTokenRefresher CodexTokenRefresher
)

const defaultCodexRefreshSkewSeconds int64 = 60

func RegisterCodexTokenRefresher(refresher CodexTokenRefresher) {
	codexRefreshMu.Lock()
	defer codexRefreshMu.Unlock()
	codexTokenRefresher = refresher
}

func codexRefreshSkew(store *config.Store) time.Duration {
	skew := defaultCodexRefreshSkewSeconds
	if store != nil {
		if configured := store.Snapshot().Codex.RefreshSkewSeconds; configured > 0 {
			skew = configured
		}
	}
	return time.Duration(skew) * time.Second
}

func applyCodexTokens(a *RequestAuth, accessToken, refreshToken, idToken string, expiresIn int64, email, accID, planType string) {
	if accessToken != "" {
		a.Account.Token = accessToken
	}
	if refreshToken != "" {
		a.Account.CodexRefreshToken = refreshToken
	}
	if idToken != "" {
		a.Account.CodexIDToken = idToken
	}
	if expiresIn > 0 {
		a.Account.CodexExpiresAt = time.Now().Unix() + expiresIn
	}
	if email != "" && a.Account.Email == "" {
		a.Account.Email = email
	}
	if accID != "" && a.Account.CodexAccountID == "" {
		a.Account.CodexAccountID = accID
	}
	if planType != "" {
		a.Account.CodexPlanType = planType
	}
}

func ensureCodexToken(ctx context.Context, a *RequestAuth, store *config.Store) error {
	if a == nil {
		return errors.New("nil auth request")
	}

	refreshToken := strings.TrimSpace(a.Account.CodexRefreshToken)
	if refreshToken == "" {
		return errors.New("codex account missing refresh token")
	}

	skew := codexRefreshSkew(store)
	if a.Account.Token != "" && time.Now().Unix() > 0 &&
		time.Unix(a.Account.CodexExpiresAt, 0).After(time.Now().Add(skew)) {
		return nil
	}

	id := a.Account.Identifier()
	if id == "" {
		id = a.Account.CodexAccountID
	}

	codexRefreshMu.Lock()
	if call, running := codexInFlight[id]; running {
		codexRefreshMu.Unlock()
		select {
		case <-call.done:
			if call.err != nil {
				return call.err
			}
			// Adopt the credentials the leader obtained; this request's account
			// copy predates the refresh.
			applyCodexTokens(a, call.accessToken, call.refreshToken, call.idToken, call.expiresIn, call.email, call.accID, call.planType)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	call := &codexRefreshCall{done: make(chan struct{})}
	codexInFlight[id] = call
	refresher := codexTokenRefresher
	codexRefreshMu.Unlock()

	defer func() {
		codexRefreshMu.Lock()
		delete(codexInFlight, id)
		codexRefreshMu.Unlock()
		close(call.done)
	}()

	if refresher == nil {
		call.err = errors.New("codex token refresher not registered")
		return call.err
	}

	accessToken, newRefreshToken, idToken, expiresIn, email, accID, planType, err := refresher(ctx, refreshToken, codexAccountProxy(store, a.Account))
	if err != nil {
		call.err = err
		if isCodexRefreshInvalidGrant(err) {
			a.Account.Disabled = true
			a.Account.DisabledReason = "codex_refresh_invalid_grant"
			if store != nil {
				_ = store.Update(func(cfg *config.Config) error {
					for i := range cfg.Accounts {
						if cfg.Accounts[i].Identifier() == a.Account.Identifier() {
							cfg.Accounts[i].Disabled = true
							cfg.Accounts[i].DisabledReason = "codex_refresh_invalid_grant"
						}
					}
					return nil
				})
			}
		}
		return err
	}

	call.accessToken = accessToken
	call.refreshToken = newRefreshToken
	call.idToken = idToken
	call.expiresIn = expiresIn
	call.email = email
	call.accID = accID
	call.planType = planType

	applyCodexTokens(a, accessToken, newRefreshToken, idToken, expiresIn, email, accID, planType)

	if store != nil {
		_ = store.Update(func(cfg *config.Config) error {
			for i := range cfg.Accounts {
				if cfg.Accounts[i].Identifier() == a.Account.Identifier() {
					cfg.Accounts[i].Token = a.Account.Token
					cfg.Accounts[i].CodexRefreshToken = a.Account.CodexRefreshToken
					cfg.Accounts[i].CodexIDToken = a.Account.CodexIDToken
					cfg.Accounts[i].CodexExpiresAt = a.Account.CodexExpiresAt
					cfg.Accounts[i].CodexAccountID = a.Account.CodexAccountID
					cfg.Accounts[i].CodexPlanType = a.Account.CodexPlanType
					if a.Account.Email != "" {
						cfg.Accounts[i].Email = a.Account.Email
					}
				}
			}
			return nil
		})
	}

	return nil
}

// codexAccountProxy resolves the proxy configured for this account, if any, so
// the OAuth refresh uses the same egress as the model requests.
func codexAccountProxy(store *config.Store, acc config.Account) *config.Proxy {
	if store == nil {
		return nil
	}
	proxyID := strings.TrimSpace(acc.ProxyID)
	if proxyID == "" {
		return nil
	}
	for _, p := range store.Snapshot().Proxies {
		if p.ID == proxyID {
			cp := config.NormalizeProxy(p)
			return &cp
		}
	}
	return nil
}

func isCodexRefreshInvalidGrant(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "refresh_token_invalidated") ||
		strings.Contains(msg, "refresh_token_reused") ||
		strings.Contains(msg, "app_session_terminated")
}

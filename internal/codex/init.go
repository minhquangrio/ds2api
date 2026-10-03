package codex

import (
	"context"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/geminiweb"
)

func init() {
	auth.RegisterCodexTokenRefresher(func(ctx context.Context, refreshToken string, proxyCfg *config.Proxy) (string, string, string, int64, string, string, string, error) {
		proxyURL := ""
		if proxyCfg != nil {
			proxyURL = geminiweb.FormatProxyURL(*proxyCfg)
		}
		tokens, err := RefreshCredentialsWithProxy(ctx, refreshToken, proxyURL)
		if err != nil {
			return "", "", "", 0, "", "", "", err
		}
		email, accID, planType := ExtractIdentity(tokens.IDToken, tokens.AccessToken)
		return tokens.AccessToken, tokens.RefreshToken, tokens.IDToken, tokens.ExpiresIn, email, accID, planType, nil
	})
}

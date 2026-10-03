package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ds2api/internal/config"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	Scope        string `json:"scope"`
}

type LoginSession struct {
	ID               string    `json:"id"`
	AuthorizeURL     string    `json:"authorize_url"`
	RedirectURI      string    `json:"redirect_uri"`
	State            string    `json:"state"`
	Verifier         string    `json:"verifier"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
	CreatedAt        time.Time `json:"created_at"`

	// mu guards every field below. Status/Error were previously written under
	// the manager lock while the completion path wrote them under this lock,
	// which is a data race on the same fields.
	mu        sync.Mutex
	status    string
	errMsg    string
	account   *config.Account
	persisted bool
	server    *http.Server
	listener  []net.Listener
}

// ClaimPersisted reports whether this caller is the first to observe the
// completed session, so the account is written to config only once instead of
// on every console poll.
func (s *LoginSession) ClaimPersisted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.persisted {
		return false
	}
	s.persisted = true
	return true
}

// LoginSessionView is the wire representation. It deliberately omits tokens:
// the console polls this endpoint and must not receive account credentials.
type LoginSessionView struct {
	ID               string            `json:"id"`
	AuthorizeURL     string            `json:"authorize_url"`
	RedirectURI      string            `json:"redirect_uri"`
	State            string            `json:"state"`
	Status           string            `json:"status"`
	ExpiresInSeconds int64             `json:"expires_in_seconds"`
	Account          *SanitizedAccount `json:"account,omitempty"`
	Error            string            `json:"error,omitempty"`
}

// SanitizedAccount reports which account was linked without exposing the access
// token, refresh token or id token.
type SanitizedAccount struct {
	Provider      string `json:"provider"`
	Email         string `json:"email,omitempty"`
	Name          string `json:"name,omitempty"`
	AccountID     string `json:"codex_account_id,omitempty"`
	PlanType      string `json:"codex_plan_type,omitempty"`
	ExpiresAt     int64  `json:"codex_expires_at,omitempty"`
	HasRefreshTok bool   `json:"has_refresh_token"`
}

func (s *LoginSession) setStatus(status, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.errMsg = errMsg
}

func (s *LoginSession) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Account returns the linked account. Callers must not mutate the result into
// shared state.
func (s *LoginSession) Account() *config.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account == nil {
		return nil
	}
	cp := *s.account
	return &cp
}

func (s *LoginSession) View() LoginSessionView {
	s.mu.Lock()
	defer s.mu.Unlock()

	view := LoginSessionView{
		ID:               s.ID,
		AuthorizeURL:     s.AuthorizeURL,
		RedirectURI:      s.RedirectURI,
		State:            s.State,
		Status:           s.status,
		ExpiresInSeconds: s.ExpiresInSeconds,
		Error:            s.errMsg,
	}
	if s.account != nil {
		view.Account = &SanitizedAccount{
			Provider:      s.account.Provider,
			Email:         s.account.Email,
			Name:          s.account.Name,
			AccountID:     s.account.CodexAccountID,
			PlanType:      s.account.CodexPlanType,
			ExpiresAt:     s.account.CodexExpiresAt,
			HasRefreshTok: s.account.CodexRefreshToken != "",
		}
	}
	return view
}

func ParseAuthorizationInput(input string) (code, state string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ""
	}

	// 1. If full URL (e.g. http://localhost:1455/auth/callback?code=...&state=...)
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err == nil {
			q := u.Query()
			code = q.Get("code")
			state = q.Get("state")
			if code != "" {
				return code, state
			}
		}
	}

	// 2. Query string style: code=foo&state=bar
	if strings.Contains(input, "code=") {
		vals, err := url.ParseQuery(input)
		if err == nil && vals.Get("code") != "" {
			return vals.Get("code"), vals.Get("state")
		}
	}

	// 3. code#state style
	if idx := strings.Index(input, "#"); idx != -1 {
		return strings.TrimSpace(input[:idx]), strings.TrimSpace(input[idx+1:])
	}

	// 4. Raw code
	return input, ""
}

func DecodeJWTPayload(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid jwt format")
	}

	payload := parts[1]
	// Handle URL encoding with or without padding
	var data []byte
	var err error
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	data, err = base64.URLEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawURLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return nil, fmt.Errorf("decode jwt payload: %w", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal jwt claims: %w", err)
	}
	return claims, nil
}

func ExtractIdentity(idToken, accessToken string) (email, accountID, planType string) {
	parseClaims := func(token string) {
		if token == "" {
			return
		}
		claims, err := DecodeJWTPayload(token)
		if err != nil {
			return
		}

		if email == "" {
			if em, ok := claims["email"].(string); ok {
				email = em
			}
		}

		if authObj, ok := claims[AuthClaimOpenAI].(map[string]any); ok {
			if accountID == "" {
				if acc, ok := authObj["chatgpt_account_id"].(string); ok {
					accountID = acc
				}
			}
			if planType == "" {
				if pt, ok := authObj["plan_type"].(string); ok {
					planType = pt
				}
			}
		}
	}

	parseClaims(idToken)
	parseClaims(accessToken)
	return email, accountID, planType
}

func ExchangeCode(ctx context.Context, code, verifier, redirectURI string) (*TokenResponse, error) {
	return ExchangeCodeWithProxy(ctx, code, verifier, redirectURI, "")
}

func ExchangeCodeWithProxy(ctx context.Context, code, verifier, redirectURI, proxyURL string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {OAuthClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "DS2API-CodexOAuth")

	client := &http.Client{Timeout: 20 * time.Second, Transport: buildTransport(proxyURL)}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close token exchange response body: %v", cerr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal token response: %w", err)
	}

	return &tr, nil
}

func RefreshCredentials(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	return RefreshCredentialsWithProxy(ctx, refreshToken, "")
}

func RefreshCredentialsWithProxy(ctx context.Context, refreshToken, proxyURL string) (*TokenResponse, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {OAuthClientID},
		"refresh_token": {refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "DS2API-CodexOAuth")

	client := &http.Client{Timeout: 20 * time.Second, Transport: buildTransport(proxyURL)}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close refresh response body: %v", cerr)
		}
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal refresh response: %w", err)
	}

	return &tr, nil
}

type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*LoginSession
}

var defaultLoginManager = &SessionManager{
	sessions: make(map[string]*LoginSession),
}

// loginSessionTTL bounds how long an unfinished login is kept and polled.
const loginSessionTTL = 15 * time.Minute

func DefaultLoginManager() *SessionManager {
	return defaultLoginManager
}

func (m *SessionManager) StartSession(port int) (*LoginSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	state := GenerateState()
	loginID := fmt.Sprintf("codex_login_%d", time.Now().UnixNano())

	if port <= 0 {
		port = 1455
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", port)

	vals := url.Values{
		"client_id":             {OAuthClientID},
		"response_type":         {"code"},
		"redirect_uri":          {redirectURI},
		"scope":                 {OAuthScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		// Parameters the Codex CLI sends; the authorize endpoint expects them to
		// select the simplified CLI consent flow.
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {HeaderOriginatorVal},
	}
	authURL := fmt.Sprintf("%s?%s", OAuthAuthorizeURL, vals.Encode())

	session := &LoginSession{
		ID:               loginID,
		AuthorizeURL:     authURL,
		RedirectURI:      redirectURI,
		State:            state,
		Verifier:         verifier,
		status:           "pending",
		ExpiresInSeconds: 900,
		CreatedAt:        time.Now(),
	}

	m.evictExpiredLocked()

	// Bind both loopback families: "localhost" resolves to ::1 on some systems,
	// and a browser redirect there would otherwise get connection refused.
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		callbackState := r.URL.Query().Get("state")

		if callbackState != session.State {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body style="font-family:sans-serif;padding:40px;text-align:center;">
<h2>Đăng nhập thành công!</h2><p>Bạn có thể đóng tab này và quay lại DS2API.</p>
</body></html>`))

		go func() {
			time.Sleep(500 * time.Millisecond)
			if cerr := m.CompleteSessionWithCode(session.ID, code); cerr != nil {
				log.Printf("codex: completing login session %s failed: %v", session.ID, cerr)
			}
		}()
	})

	server := &http.Server{Handler: mux}
	for _, addr := range []string{
		fmt.Sprintf("127.0.0.1:%d", port),
		fmt.Sprintf("[::1]:%d", port),
	} {
		listener, lerr := net.Listen("tcp", addr)
		if lerr != nil {
			continue
		}
		session.addListener(listener)
		go func(l net.Listener) {
			if serr := server.Serve(l); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
				log.Printf("codex callback listener exited: %v", serr)
			}
		}(listener)
	}
	if len(session.listeners()) > 0 {
		session.setServer(server)
	}

	m.sessions[loginID] = session
	return session, nil
}

// evictExpiredLocked drops sessions nobody will poll again. Callers must hold m.mu.
func (m *SessionManager) evictExpiredLocked() {
	for id, s := range m.sessions {
		if time.Since(s.CreatedAt) > loginSessionTTL {
			s.closeServer()
			delete(m.sessions, id)
		}
	}
}

func (m *SessionManager) GetSession(id string) (*LoginSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Since(s.CreatedAt) > loginSessionTTL {
		s.setStatus("failed", "login session expired")
		s.closeServer()
	}
	return s, true
}

func (m *SessionManager) CancelSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return errors.New("session not found")
	}

	s.setStatus("cancelled", "")
	s.closeServer()
	return nil
}

func (m *SessionManager) CompleteSessionWithCode(id, code string) error {
	s, ok := m.GetSession(id)
	if !ok {
		return errors.New("session not found")
	}

	if s.Status() == "completed" {
		return nil
	}

	s.mu.Lock()
	if s.status == "completed" {
		s.mu.Unlock()
		return nil
	}
	if s.status == "cancelled" {
		s.mu.Unlock()
		return errors.New("login session was cancelled")
	}
	verifier, redirectURI := s.Verifier, s.RedirectURI
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tokens, err := ExchangeCode(ctx, code, verifier, redirectURI)
	if err != nil {
		s.setStatus("failed", redactTokenExchangeError(err))
		s.closeServer()
		return err
	}

	email, accID, planType := ExtractIdentity(tokens.IDToken, tokens.AccessToken)

	// The account id identifies the record; without one the console would show
	// an unnamed entry and a later re-login could not be matched back to it.
	if accID == "" && email == "" {
		s.setStatus("failed", "login succeeded but the token carried no account identity")
		s.closeServer()
		return errors.New("could not derive account identity from the returned tokens")
	}

	expiresAt := time.Now().Unix() + tokens.ExpiresIn

	name := "codex:" + accID
	if accID == "" {
		name = "codex:" + email
	}

	acc := &config.Account{
		Provider:           "codex",
		Email:              email,
		Name:               name,
		Token:              tokens.AccessToken,
		CodexRefreshToken:  tokens.RefreshToken,
		CodexIDToken:       tokens.IDToken,
		CodexExpiresAt:     expiresAt,
		CodexAccountID:     accID,
		CodexPlanType:      planType,
		CodexAccountSource: "chatgpt_account_id",
	}

	s.mu.Lock()
	s.account = acc
	s.status = "completed"
	s.errMsg = ""
	s.mu.Unlock()
	s.closeServer()
	return nil
}

func (m *SessionManager) CompleteSessionWithInput(id, input string) error {
	code, state := ParseAuthorizationInput(input)
	if code == "" {
		return errors.New("could not extract code from input")
	}

	s, ok := m.GetSession(id)
	if !ok {
		return errors.New("session not found")
	}

	if state != "" && state != s.State {
		return errors.New("state parameter does not match session")
	}

	return m.CompleteSessionWithCode(id, code)
}

func (s *LoginSession) addListener(l net.Listener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listener = append(s.listener, l)
}

func (s *LoginSession) listeners() []net.Listener {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]net.Listener(nil), s.listener...)
}

func (s *LoginSession) setServer(srv *http.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.server = srv
}

func (s *LoginSession) closeServer() {
	s.mu.Lock()
	server := s.server
	listeners := s.listener
	s.server = nil
	s.listener = nil
	s.mu.Unlock()

	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("codex: callback server shutdown: %v", err)
		}
	}
	for _, l := range listeners {
		if l == nil {
			continue
		}
		if cerr := l.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			log.Printf("codex: failed to close listener: %v", cerr)
		}
	}
}

// redactTokenExchangeError keeps the upstream error useful without pasting the
// whole response body into the console.
func redactTokenExchangeError(err error) string {
	msg := err.Error()
	if len(msg) > 512 {
		msg = msg[:512] + "… (truncated)"
	}
	return msg
}

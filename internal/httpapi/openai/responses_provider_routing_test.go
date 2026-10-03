package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
)

// providerCapturingAuth records the provider the handler selected before it
// resolved auth. The OpenAI Responses endpoint must derive this from the
// requested model; otherwise provider-specific models are routed to DeepSeek.
type providerCapturingAuth struct {
	seenProvider string
}

func (s *providerCapturingAuth) resolve(r *http.Request) (*auth.RequestAuth, error) {
	s.seenProvider = r.Header.Get("X-Ds2-Target-Provider")
	if s.seenProvider != "codex" {
		// Stop before the DeepSeek completion path; this stub only exists to
		// observe which provider the handler selected.
		return nil, auth.ErrUnauthorized
	}
	return &auth.RequestAuth{
		UseConfigToken: false,
		Provider:       s.seenProvider,
		DeepSeekToken:  "direct-token",
		CallerID:       "caller:test",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (s *providerCapturingAuth) Determine(r *http.Request) (*auth.RequestAuth, error) {
	return s.resolve(r)
}

func (s *providerCapturingAuth) DetermineCaller(r *http.Request) (*auth.RequestAuth, error) {
	return s.resolve(r)
}

func (s *providerCapturingAuth) DetermineForProvider(r *http.Request, _ string) (*auth.RequestAuth, error) {
	return s.resolve(r)
}

func (s *providerCapturingAuth) Release(_ *auth.RequestAuth)                         {}
func (s *providerCapturingAuth) ToolsEnabledForRequest(_ *http.Request) bool         { return true }
func (s *providerCapturingAuth) SetAccountMutedUntil(_ *auth.RequestAuth, _ float64) {}
func (s *providerCapturingAuth) SetAccountBanned(_ *auth.RequestAuth, _ string)      {}
func (s *providerCapturingAuth) EnforceKeyModelQuota(_ *http.Request, _ auth.CallerTokenReader, _ string) error {
	return nil
}

func TestResponsesResolvesProviderFromModel(t *testing.T) {
	cases := []struct {
		name     string
		model    string
		provider string
	}{
		{name: "codex catalogue model", model: "gpt-6-luna", provider: "codex"},
		{name: "codex prefix escape hatch", model: "codex/any-model", provider: "codex"},
		{name: "deepseek model", model: "deepseek-v4-flash", provider: "deepseek"},
		{name: "gemini model", model: "gemini-3.0-pro"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capture := &providerCapturingAuth{}
			h := &openAITestSurface{
				Store: mockOpenAIConfig{},
				Auth:  capture,
				DS:    streamStatusDSStub{},
			}
			r := chi.NewRouter()
			registerOpenAITestRoutes(r, h)

			body := `{"model":"` + tc.model + `","input":"hi","stream":false}`
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer direct-token")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if tc.provider == "" {
				// A provider we do not serve must not be claimed.
				if capture.seenProvider == "codex" {
					t.Fatalf("model %q must not resolve to codex", tc.model)
				}
				return
			}
			if capture.seenProvider != tc.provider {
				t.Fatalf("model %q resolved provider %q; want %q", tc.model, capture.seenProvider, tc.provider)
			}
			if tc.provider == "codex" {
				// The codex branch must actually be entered: the stub has no
				// usable codex account, so it fails with a gateway error rather
				// than falling through to the DeepSeek path (which would 401).
				if rec.Code != http.StatusBadGateway {
					t.Fatalf("codex model reached status %d; want %d (branch not taken?)",
						rec.Code, http.StatusBadGateway)
				}
			}
		})
	}
}

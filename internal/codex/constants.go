package codex

const (
	OAuthClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	OAuthAuthorizeURL  = "https://auth.openai.com/oauth/authorize"
	OAuthTokenURL      = "https://auth.openai.com/oauth/token"
	DefaultRedirectURI = "http://localhost:1455/auth/callback"
	OAuthScope         = "openid profile email offline_access"

	CodexResponsesURL = "https://chatgpt.com/backend-api/codex/responses"
	CodexModelsURL    = "https://chatgpt.com/backend-api/codex/models?client_version=1.0.0"

	HeaderOpenAIBeta     = "OpenAI-Beta"
	HeaderOpenAIBetaVal  = "responses=experimental"
	HeaderOriginator     = "Originator"
	HeaderOriginatorVal  = "pi"
	HeaderChatGPTAccount = "ChatGPT-Account-Id"
	AuthClaimOpenAI      = "https://api.openai.com/auth"
)

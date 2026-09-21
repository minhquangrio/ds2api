package geminiweb

import (
	"regexp"
)

const (
	BaseURL           = "https://gemini.google.com"
	AppURL            = "https://gemini.google.com/app"
	BatchExecuteURL   = "https://gemini.google.com/_/BardChatUi/data/batchexecute"
	StreamGenerateURL = "https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate"

	// UploadURL and RotateCookiesURL live on other origins than the rest of the
	// Gemini Web endpoints - uploads go to content-push, cookie rotation to the
	// Google account host. Pointing either at gemini.google.com fails.
	UploadURL        = "https://content-push.googleapis.com/upload"
	RotateCookiesURL = "https://accounts.google.com/RotateCookies"

	// RotateCookiesOrigin is the Origin header Google expects on rotation.
	RotateCookiesOrigin = "https://accounts.google.com"

	// RotateCookiesBody is the fixed opaque payload the rotation endpoint takes.
	RotateCookiesBody = `[000,"-0000000000000000000"]`

	RPCGetUserStatus = "otAQ7b"
	RPCGetQuota      = "qpEbW"

	HeaderExtModel   = "x-goog-ext-525001261-jspb"
	HeaderExtParam1  = "x-goog-ext-73010989-jspb"
	HeaderExtParam2  = "x-goog-ext-73010990-jspb"
	HeaderExtUUID    = "x-goog-ext-525005358-jspb"
	HeaderSameDomain = "X-Same-Domain"

	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
)

var (
	AccessTokenRe = regexp.MustCompile(`"SNlM0e":\s*"([^"]+)"`)
	BuildLabelRe  = regexp.MustCompile(`"cfb2h":\s*"([^"]+)"`)
	SessionIDRe   = regexp.MustCompile(`"FdrFJe":\s*"([^"]+)"`)
	LanguageRe    = regexp.MustCompile(`"TuX5cc":\s*"([^"]+)"`)
	PushIDRe      = regexp.MustCompile(`"qKIAYe":\s*"([^"]+)"`)
)

// Action IDs for Quota RPC (qpEbW)
const (
	QuotaActionPro           = 4
	QuotaActionFlash         = 11
	QuotaActionFlashThinking = 15
)

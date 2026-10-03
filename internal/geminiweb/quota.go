package geminiweb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	httpcloak "github.com/sardanioss/httpcloak/client"

	"ds2api/internal/config"
)

const (
	FlashQuotaPayload    = "[[[1,11],[2,11],[6,11]]]"
	AdvancedQuotaPayload = "[[[1,4],[6,6],[1,15]]]"
)

type QuotaInfo struct {
	ActionID        int     `json:"action_id"`
	QuotaID         string  `json:"quota_id"`
	Label           string  `json:"label"`
	Remaining       int     `json:"remaining"`
	Total           int     `json:"total"`
	ResetTime       int64   `json:"reset_time"`
	UsagePercentage float64 `json:"usage_percentage"`
	UsageLevel      float64 `json:"usage_level"`
	IsUnlimited     bool    `json:"is_unlimited"`
}

type quotaPayload struct {
	Payload  string
	Category string
}

var defaultQuotaPayloads = []quotaPayload{
	{Payload: FlashQuotaPayload, Category: "Flash"},
	{Payload: AdvancedQuotaPayload, Category: "Pro"},
}

type quotaFetchFunc func(ctx context.Context, session SessionParams, payload, category string) (map[string]QuotaInfo, error)

// collectQuotaPayloads merges the per-payload quota maps and aggregates errors,
// so a payload that fails no longer disappears silently.
func collectQuotaPayloads(ctx context.Context, session SessionParams, payloads []quotaPayload, fetch quotaFetchFunc) (map[string]QuotaInfo, error) {
	result := make(map[string]QuotaInfo)
	var errs []string
	for _, p := range payloads {
		quotas, err := fetch(ctx, session, p.Payload, p.Category)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p.Category, err))
			continue
		}
		for k, v := range quotas {
			result[k] = v
		}
	}
	if len(result) == 0 {
		if len(errs) == 0 {
			return nil, fmt.Errorf("quota RPC returned no items")
		}
		return nil, fmt.Errorf("quota RPC failed: %s", strings.Join(errs, "; "))
	}
	if len(errs) > 0 {
		return result, fmt.Errorf("partial quota failure: %s", strings.Join(errs, "; "))
	}
	return result, nil
}

// CheckQuota queries Gemini for Flash and Pro/Advanced quotas via RPC qpEbW.
func (c *Client) CheckQuota(ctx context.Context) (map[string]QuotaInfo, error) {
	session := c.Session()
	if session.AccessToken == "" {
		return nil, fmt.Errorf("session not initialized")
	}
	return collectQuotaPayloads(ctx, session, defaultQuotaPayloads, c.fetchQuotaPayload)
}

func (c *Client) fetchQuotaPayload(ctx context.Context, session SessionParams, payload, category string) (map[string]QuotaInfo, error) {
	raw, err := c.batchExecuteRPC(ctx, session, RPCGetQuota, payload, "/app")
	if err != nil {
		return nil, err
	}
	quotas := ParseQuotaResponse(raw, category)
	if len(quotas) == 0 {
		return nil, fmt.Errorf("no quota items parsed from response")
	}
	return quotas, nil
}

func (c *Client) batchExecuteRPC(ctx context.Context, session SessionParams, rpcID, payload, sourcePath string) (string, error) {
	params := url.Values{
		"rpcids":      {rpcID},
		"hl":          {session.Language},
		"_reqid":      {"100002"},
		"rt":          {"c"},
		"source-path": {sourcePath},
		"bl":          {session.BuildLabel},
	}
	if session.SessionID != "" {
		params.Set("f.sid", session.SessionID)
	}

	reqURL := BatchExecuteURL + "?" + params.Encode()
	headers := c.BuildDefaultHeaders()
	headers["Content-Type"] = []string{"application/x-www-form-urlencoded;charset=utf-8"}

	escapedPayload, _ := json.Marshal(payload)
	batchPayload := fmt.Sprintf(`[[["%s",%s,null,"generic"]]]`, rpcID, string(escapedPayload))

	postForm := url.Values{
		"at":    {session.AccessToken},
		"f.req": {batchPayload},
	}

	req := &httpcloak.Request{
		Method:  http.MethodPost,
		URL:     reqURL,
		Headers: headers,
		Body:    strings.NewReader(postForm.Encode()),
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			config.Logger.Warn("[geminiweb] failed to close response body in batchExecuteRPC", "rpc", rpcID, "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("RPC %s status %d", rpcID, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	c.checkSetCookies(resp.Headers)
	return string(bodyBytes), nil
}

// idSegment renders one element of the quota id list as text.
// Gemini returns these as either strings ("1") or numbers (1).
func idSegment(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return "", false
		}
		return t, true
	case int:
		return strconv.Itoa(t), true
	case float64:
		return strconv.Itoa(int(t)), true
	case json.Number:
		return t.String(), true
	}
	return "", false
}

// ParseQuotaResponse parses the batchexecute response string for qpEbW.
func ParseQuotaResponse(raw, category string) map[string]QuotaInfo {
	result := make(map[string]QuotaInfo)
	partBody, ok := parseRPCBody(raw, RPCGetQuota)
	if !ok || len(partBody) == 0 {
		return result
	}

	quotaItems, ok := partBody[0].([]any)
	if !ok {
		return result
	}

	for _, itemRaw := range quotaItems {
		item, ok := itemRaw.([]any)
		if !ok || len(item) < 6 {
			continue
		}

		idList, ok := item[0].([]any)
		if !ok || len(idList) < 2 {
			continue
		}

		seg0, ok0 := idSegment(idList[0])
		seg1, ok1 := idSegment(idList[1])
		if !ok0 || !ok1 {
			continue
		}

		quotaID := seg0 + "-" + seg1
		actionID, _ := strconv.Atoi(seg1)

		rawLevel, _ := item[2].(float64)
		resetTs := int64(0)
		if resetList, ok := item[3].([]any); ok && len(resetList) > 0 {
			if r, ok := resetList[0].(float64); ok {
				resetTs = int64(r)
			}
		}
		total := 0
		if t, ok := item[4].(float64); ok {
			total = int(t)
		}
		remaining := 0
		if rem, ok := item[5].(float64); ok {
			remaining = int(rem)
		}
		isUnlimited := total == 0 && remaining == 0

		usagePct := rawLevel
		if total > 0 {
			usagePct = float64(total-remaining) / float64(total) * 100
			if usagePct < 0 {
				usagePct = 0
			}
			if usagePct > 100 {
				usagePct = 100
			}
		}

		label := fmt.Sprintf("Gemini %s", category)
		switch actionID {
		case QuotaActionPro:
			label = "Gemini Pro"
		case QuotaActionFlash:
			label = "Gemini Flash"
		case QuotaActionFlashThinking:
			label = "Gemini Flash Thinking"
		}
		label = fmt.Sprintf("%s [%s]", label, quotaID)

		result[quotaID] = QuotaInfo{
			ActionID:        actionID,
			QuotaID:         quotaID,
			Label:           label,
			Remaining:       remaining,
			Total:           total,
			ResetTime:       resetTs,
			UsagePercentage: usagePct,
			UsageLevel:      rawLevel,
			IsUnlimited:     isUnlimited,
		}
	}
	return result
}

// cloneQuotaSummary returns a shallow copy so callers can set Identifier
// without mutating the cached instance. Callers must not mutate the maps.
func cloneQuotaSummary(in *GeminiAccountQuotaSummary) *GeminiAccountQuotaSummary {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

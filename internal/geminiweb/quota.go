package geminiweb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	httpcloak "github.com/sardanioss/httpcloak/client"
)

const (
	FlashQuotaPayload    = "[[[1,11],[2,11],[6,11]]]"
	AdvancedQuotaPayload = "[[[1,4],[6,6],[1,15]]]"
)

type QuotaInfo struct {
	ActionID        int     `json:"action_id"`
	Label           string  `json:"label"`
	Remaining       int     `json:"remaining"`
	Total           int     `json:"total"`
	ResetTime       int64   `json:"reset_time"`
	UsagePercentage float64 `json:"usage_percentage"`
	IsUnlimited     bool    `json:"is_unlimited"`
}

// CheckQuota queries Gemini for Flash and Pro/Advanced quotas via RPC qpEbW.
func (c *Client) CheckQuota(ctx context.Context) (map[int]QuotaInfo, error) {
	session := c.Session()
	if session.AccessToken == "" {
		return nil, fmt.Errorf("session not initialized")
	}

	result := make(map[int]QuotaInfo)
	payloads := []string{FlashQuotaPayload, AdvancedQuotaPayload}

	for _, p := range payloads {
		quotas, err := c.fetchQuotaPayload(ctx, session, p)
		if err != nil {
			continue
		}
		for k, v := range quotas {
			result[k] = v
		}
	}

	return result, nil
}

func (c *Client) fetchQuotaPayload(ctx context.Context, session SessionParams, payload string) (map[int]QuotaInfo, error) {
	raw, err := c.batchExecuteRPC(ctx, session, RPCGetQuota, payload, "/app")
	if err != nil {
		return nil, err
	}
	return ParseQuotaResponse(raw), nil
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
	defer func() { _ = resp.Body.Close() }()

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

// ParseQuotaResponse parses the batchexecute response string for qpEbW.
func ParseQuotaResponse(raw string) map[int]QuotaInfo {
	result := make(map[int]QuotaInfo)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `[["wrb.fr","`+RPCGetQuota) {
			continue
		}
		var outer [][]any
		if err := json.Unmarshal([]byte(line), &outer); err != nil || len(outer) == 0 || len(outer[0]) < 3 {
			continue
		}
		payloadStr, ok := outer[0][2].(string)
		if !ok || payloadStr == "" {
			continue
		}
		var inner []any
		if err := json.Unmarshal([]byte(payloadStr), &inner); err != nil || len(inner) == 0 {
			continue
		}
		quotaItems, ok := inner[0].([]any)
		if !ok {
			continue
		}

		for _, itemRaw := range quotaItems {
			item, ok := itemRaw.([]any)
			if !ok || len(item) < 6 {
				continue
			}
			idList, _ := item[0].([]any)
			actionID := 0
			if len(idList) > 1 {
				if aid, ok := idList[1].(float64); ok {
					actionID = int(aid)
				}
			}

			usagePct, _ := item[2].(float64)
			usagePct = usagePct * 100

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

			label := "Gemini"
			switch actionID {
			case QuotaActionPro:
				label = "Gemini Pro"
			case QuotaActionFlash:
				label = "Gemini Flash"
			case QuotaActionFlashThinking:
				label = "Gemini Flash Thinking"
			}

			result[actionID] = QuotaInfo{
				ActionID:        actionID,
				Label:           label,
				Remaining:       remaining,
				Total:           total,
				ResetTime:       resetTs,
				UsagePercentage: usagePct,
				IsUnlimited:     isUnlimited,
			}
		}
	}
	return result
}

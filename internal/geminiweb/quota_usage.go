package geminiweb

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

var TierLabels = map[int]string{
	1: "FREE", 2: "PRO", 3: "ULTRA", 4: "PLUS", 6: "ULTRA",
}

type UsageMetric struct {
	Type             int      `json:"type"`
	Window           string   `json:"window"`
	RemainingCredits *int     `json:"remaining_credits"`
	UsageLevel       *float64 `json:"usage_level"`
	UsagePercentage  *int     `json:"usage_percentage"` // 0-100
	ResetAt          string   `json:"reset_at"`
	ResetTimestamp   int64    `json:"reset_timestamp"`
}

type TierInfo struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

type UsageInfo struct {
	Tier                TierInfo     `json:"tier"`
	UseOverageAICredits any          `json:"use_overage_ai_credits"`
	Current5h           *UsageMetric `json:"current_5h"`
	Weekly              *UsageMetric `json:"weekly"`
	AICreditsRemaining  *int         `json:"ai_credits_remaining"`
}

type ExtraQuotaInfo struct {
	IsBlocked       bool     `json:"is_blocked"`
	UsagePercentage *float64 `json:"usage_percentage"` // 0-100
	ResetTime       int64    `json:"reset_time"`
	ResetAt         string   `json:"reset_at"`
}

type GeminiAccountQuotaSummary struct {
	Identifier    string            `json:"identifier"`
	Provider      string            `json:"provider"`
	Tier          TierInfo          `json:"tier"`
	Usage         UsageInfo         `json:"usage"`
	Quotas        map[int]QuotaInfo `json:"quotas"`
	ExtraFeatures ExtraQuotaInfo    `json:"extra_features"`
	PartialErrors []string          `json:"partial_errors,omitempty"`
	FetchedAt     string            `json:"fetched_at"`
}

func parseRPCBody(raw, targetRPC string) ([]any, bool) {
	for _, part := range extractJSONFrames(raw) {
		partList, ok := part.([]any)
		if !ok || len(partList) < 3 {
			continue
		}
		if rpcID, ok := partList[1].(string); ok && rpcID != "" && rpcID != targetRPC {
			continue
		}
		bodyStr, ok := partList[2].(string)
		if !ok || bodyStr == "" {
			continue
		}
		var body []any
		if err := json.Unmarshal([]byte(bodyStr), &body); err == nil && len(body) > 0 {
			return body, true
		}
	}
	return nil, false
}

func parseTimestamp(v any) (int64, string) {
	if l, ok := v.([]any); ok && len(l) > 0 {
		if sub, ok := l[0].([]any); ok && len(sub) > 0 {
			if ts, ok := sub[0].(float64); ok && ts > 0 {
				return int64(ts), time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
			}
		} else if ts, ok := l[0].(float64); ok && ts > 0 {
			return int64(ts), time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
		}
	}
	return 0, ""
}

func ParseUsageInfoResponse(raw string) *UsageInfo {
	partBody, ok := parseRPCBody(raw, RPCGetUsageInfo)
	if !ok {
		return nil
	}

	tierID := 0
	if tid, ok := partBody[0].(float64); ok {
		tierID = int(tid)
	}
	tierLabel := TierLabels[tierID]
	if tierLabel == "" && tierID > 0 {
		tierLabel = fmt.Sprintf("TIER_%d", tierID)
	}

	var useOverage any
	if len(partBody) > 2 {
		useOverage = partBody[2]
	}

	usage := &UsageInfo{
		Tier:                TierInfo{ID: tierID, Label: tierLabel},
		UseOverageAICredits: useOverage,
	}

	if len(partBody) <= 1 {
		return usage
	}
	usageItems, _ := partBody[1].([]any)
	for _, itemRaw := range usageItems {
		item, ok := itemRaw.([]any)
		if !ok || len(item) < 3 {
			continue
		}

		var remaining *int
		if r, ok := item[0].(float64); ok {
			rem := int(r)
			remaining = &rem
		}
		var usageLevel *float64
		var usagePct *int
		if ul, ok := item[1].(float64); ok {
			usageLevel = &ul
			p := int(math.Round(ul * 100))
			usagePct = &p
		}
		metricType := 0
		if mt, ok := item[2].(float64); ok {
			metricType = int(mt)
		}
		if metricType == MetricTypeAICredits {
			usage.AICreditsRemaining = remaining
			continue
		}

		var resetTs int64
		var resetAt string
		if len(item) > 3 {
			resetTs, resetAt = parseTimestamp(item[3])
		}

		metric := &UsageMetric{
			Type:             metricType,
			RemainingCredits: remaining,
			UsageLevel:       usageLevel,
			UsagePercentage:  usagePct,
			ResetAt:          resetAt,
			ResetTimestamp:   resetTs,
		}

		switch metricType {
		case MetricType5h:
			metric.Window = "5h"
			usage.Current5h = metric
		case MetricTypeWeekly:
			metric.Window = "weekly"
			usage.Weekly = metric
		default:
			metric.Window = fmt.Sprintf("type_%d", metricType)
		}
	}
	return usage
}

func ParseExtraQuotaResponse(raw string) *ExtraQuotaInfo {
	partBody, ok := parseRPCBody(raw, RPCCheckExtraQuota)
	if !ok {
		return nil
	}

	isBlocked := false
	if b, ok := partBody[0].(bool); ok {
		isBlocked = b
	}
	var usagePct *float64
	if len(partBody) > 1 {
		if ul, ok := partBody[1].(float64); ok {
			pct := ul * 100
			usagePct = &pct
		}
	}
	var resetTs int64
	var resetAt string
	if len(partBody) > 2 {
		resetTs, resetAt = parseTimestamp(partBody[2])
	}
	return &ExtraQuotaInfo{
		IsBlocked:       isBlocked,
		UsagePercentage: usagePct,
		ResetTime:       resetTs,
		ResetAt:         resetAt,
	}
}

func (c *Client) FetchUsageInfo(ctx context.Context) (*UsageInfo, error) {
	session := c.Session()
	if session.AccessToken == "" {
		return nil, fmt.Errorf("session not initialized")
	}
	raw, err := c.batchExecuteRPC(ctx, session, RPCGetUsageInfo, "[]", "/usage")
	if err != nil {
		return nil, err
	}
	info := ParseUsageInfoResponse(raw)
	if info == nil {
		return nil, fmt.Errorf("failed to parse usage info from response")
	}
	return info, nil
}

func (c *Client) FetchExtraQuota(ctx context.Context) (*ExtraQuotaInfo, error) {
	session := c.Session()
	if session.AccessToken == "" {
		return nil, fmt.Errorf("session not initialized")
	}
	raw, err := c.batchExecuteRPC(ctx, session, RPCCheckExtraQuota, "[]", "/app")
	if err != nil {
		return nil, err
	}
	info := ParseExtraQuotaResponse(raw)
	if info == nil {
		return nil, fmt.Errorf("failed to parse extra quota from response")
	}
	return info, nil
}

func (c *Client) GetFullQuota(ctx context.Context, forceRefresh bool) (*GeminiAccountQuotaSummary, error) {
	if !forceRefresh {
		c.mu.RLock()
		cached := c.quotaCache
		cachedTime := c.quotaCached
		c.mu.RUnlock()
		if cached != nil && time.Since(cachedTime) < 45*time.Second {
			return cached, nil
		}
	}

	var (
		usage      *UsageInfo
		quotas     map[int]QuotaInfo
		extra      *ExtraQuotaInfo
		partErrors []string
	)

	fetchSub := func(fn func() error, name string) {
		if err := fn(); err != nil {
			partErrors = append(partErrors, fmt.Sprintf("%s: %v", name, err))
		}
	}
	fetchSub(func() (err error) { usage, err = c.FetchUsageInfo(ctx); return }, "usage info")
	fetchSub(func() (err error) { quotas, err = c.CheckQuota(ctx); return }, "model quota")
	fetchSub(func() (err error) { extra, err = c.FetchExtraQuota(ctx); return }, "extra quota")

	if usage == nil && len(quotas) == 0 && extra == nil {
		return nil, fmt.Errorf("all quota RPCs failed: %s", strings.Join(partErrors, "; "))
	}

	summary := &GeminiAccountQuotaSummary{
		Provider:      "gemini",
		FetchedAt:     time.Now().UTC().Format(time.RFC3339),
		PartialErrors: partErrors,
		Quotas:        quotas,
	}
	if summary.Quotas == nil {
		summary.Quotas = make(map[int]QuotaInfo)
	}
	if usage != nil {
		summary.Tier = usage.Tier
		summary.Usage = *usage
	}
	if extra != nil {
		summary.ExtraFeatures = *extra
	}

	c.mu.Lock()
	c.quotaCache = summary
	c.quotaCached = time.Now()
	c.mu.Unlock()

	return summary, nil
}

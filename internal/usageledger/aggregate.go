package usageledger

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

type QueryOptions struct {
	RangeKey          string
	StartMs           int64
	EndMs             int64
	Top               int
	Recent            int
	TimezoneOffsetMin int
}

type ModelStat struct {
	Name             string `json:"name"`
	Count            int64  `json:"count"`
	Tokens           int64  `json:"tokens"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	ReasoningTokens  int64  `json:"reasoning_tokens"`
	Percentage       int    `json:"percentage"`
}

type EntityStat struct {
	ID         string `json:"id"`
	Count      int64  `json:"count"`
	Tokens     int64  `json:"tokens"`
	Percentage int    `json:"percentage"`
}

type TimelineBucket struct {
	StartMs      int64    `json:"start_ms"`
	EndMs        int64    `json:"end_ms"`
	Prompt       int64    `json:"prompt"`
	Completion   int64    `json:"completion"`
	Reasoning    int64    `json:"reasoning"`
	Total        int64    `json:"total"`
	Count        int64    `json:"count"`
	AvgLatencyMs int64    `json:"avg_latency_ms"`
	Models       []string `json:"models"`

	elapsedMs    int64
	elapsedCount int64
}

type TotalsResponse struct {
	Requests         int64   `json:"requests"`
	Success          int64   `json:"success"`
	Errors           int64   `json:"errors"`
	Stopped          int64   `json:"stopped"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	AvgLatencyMs     int64   `json:"avg_latency_ms"`
	SuccessRate      int     `json:"success_rate"`
	TokensPerSec     float64 `json:"tokens_per_sec"`
	TokensPerMin     int64   `json:"tokens_per_min"`
	FirstMs          int64   `json:"first_ms"`
	LastMs           int64   `json:"last_ms"`
}

type RangeInfo struct {
	Key           string `json:"key"`
	StartMs       int64  `json:"start_ms"`
	EndMs         int64  `json:"end_ms"`
	Tier          Tier   `json:"tier"`
	BucketWidthMs int64  `json:"bucket_width_ms"`
	BucketCount   int    `json:"bucket_count"`
	Truncated     bool   `json:"truncated"`
}

type QueryResult struct {
	Version   int              `json:"version"`
	Revision  int64            `json:"revision"`
	FlushedAt int64            `json:"flushed_at"`
	Path      string           `json:"path"`
	Backfill  BackfillMarker   `json:"backfill"`
	Range     RangeInfo        `json:"range"`
	Totals    TotalsResponse   `json:"totals"`
	Models    []ModelStat      `json:"models"`
	Accounts  []EntityStat     `json:"accounts"`
	Callers   []EntityStat     `json:"callers"`
	Timeline  []TimelineBucket `json:"timeline"`
	Recent    []Record         `json:"recent"`
}

func (s *Store) Query(opts QueryOptions) (QueryResult, error) {
	if s == nil {
		return QueryResult{}, errors.New("usage ledger store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return QueryResult{}, s.err
	}

	rangeKey := strings.TrimSpace(opts.RangeKey)
	if rangeKey == "" {
		rangeKey = "all"
	}
	switch rangeKey {
	case "15m", "1h", "24h", "7d", "30d", "all", "custom":
	default:
		return QueryResult{}, errors.New("invalid range key: " + rangeKey)
	}

	nowMs := time.Now().UnixMilli()
	startMs := opts.StartMs
	endMs := opts.EndMs
	if rangeKey == "custom" {
		if startMs <= 0 || endMs <= 0 || startMs >= endMs {
			return QueryResult{}, errors.New("invalid custom range start/end")
		}
	}

	top := opts.Top
	if top <= 0 {
		top = 10
	} else if top > 50 {
		top = 50
	}

	recentLimit := opts.Recent
	if recentLimit <= 0 {
		recentLimit = 20
	} else if recentLimit > 100 {
		recentLimit = 100
	}

	plan := PlanRange(rangeKey, startMs, endMs, nowMs, s.totals.FirstMs)
	if plan.BucketCount <= 0 {
		return QueryResult{}, errors.New("failed to calculate bucket plan")
	}

	// 1. Gather buckets from the chosen tier
	var sourceBuckets []*Bucket
	switch plan.Tier {
	case TierMinute:
		for _, b := range s.minutes {
			if b.StartMs+MinuteWidthMs > plan.StartMs && b.StartMs < plan.EndMs {
				sourceBuckets = append(sourceBuckets, b)
			}
		}
	case TierHour:
		for _, shard := range s.hours {
			for _, b := range shard {
				if b.StartMs+HourWidthMs > plan.StartMs && b.StartMs < plan.EndMs {
					sourceBuckets = append(sourceBuckets, b)
				}
			}
		}
	case TierDay:
		for _, shard := range s.days {
			for _, b := range shard {
				if b.StartMs+DayWidthMs > plan.StartMs && b.StartMs < plan.EndMs {
					sourceBuckets = append(sourceBuckets, b)
				}
			}
		}
	}

	// 2. Sort source buckets by StartMs
	sort.Slice(sourceBuckets, func(i, j int) bool {
		return sourceBuckets[i].StartMs < sourceBuckets[j].StartMs
	})

	// 3. Initialize timeline buckets
	timeline := make([]TimelineBucket, plan.BucketCount)
	timelineModelSets := make([]map[string]struct{}, plan.BucketCount)
	for i := 0; i < plan.BucketCount; i++ {
		bStart := plan.StartMs + int64(i)*plan.BucketWidthMs
		bEnd := bStart + plan.BucketWidthMs
		timeline[i] = TimelineBucket{
			StartMs: bStart,
			EndMs:   bEnd,
			Models:  make([]string, 0),
		}
		timelineModelSets[i] = make(map[string]struct{})
	}

	// Aggregated totals and dimensions across the query window
	var queryTotals Counters
	modelsMap := make(map[string]*ModelStat)
	accountsMap := make(map[string]*EntityStat)
	callersMap := make(map[string]*EntityStat)

	for _, b := range sourceBuckets {
		queryTotals.AddCounters(b.Counters)

		idx := int((b.StartMs - plan.StartMs) / plan.BucketWidthMs)
		if idx < 0 {
			idx = 0
		} else if idx >= plan.BucketCount {
			idx = plan.BucketCount - 1
		}

		tb := &timeline[idx]
		tb.Prompt += b.PromptTokens
		tb.Completion += b.CompletionTokens
		tb.Reasoning += b.ReasoningTokens
		tb.Total += b.TotalTokens
		tb.Count += b.Requests
		tb.elapsedMs += b.ElapsedMs
		tb.elapsedCount += b.ElapsedCount

		if len(b.Models) > 0 {
			for name, c := range b.Models {
				timelineModelSets[idx][name] = struct{}{}
				mStat := modelsMap[name]
				if mStat == nil {
					mStat = &ModelStat{Name: name}
					modelsMap[name] = mStat
				}
				mStat.Count += c.Requests
				mStat.Tokens += c.TotalTokens
				mStat.PromptTokens += c.PromptTokens
				mStat.CompletionTokens += c.CompletionTokens
				mStat.ReasoningTokens += c.ReasoningTokens
			}
		}
		if len(b.Accounts) > 0 {
			for acc, c := range b.Accounts {
				aStat := accountsMap[acc]
				if aStat == nil {
					aStat = &EntityStat{ID: acc}
					accountsMap[acc] = aStat
				}
				aStat.Count += c.Requests
				aStat.Tokens += c.TotalTokens
			}
		}
		if len(b.Callers) > 0 {
			for caller, c := range b.Callers {
				cStat := callersMap[caller]
				if cStat == nil {
					cStat = &EntityStat{ID: caller}
					callersMap[caller] = cStat
				}
				cStat.Count += c.Requests
				cStat.Tokens += c.TotalTokens
			}
		}
	}

	// For minute tier (which stores no dimensions on disk), backfill dimensions from recent ring
	if plan.Tier == TierMinute {
		for _, rec := range s.recent {
			if rec.At >= plan.StartMs && rec.At <= plan.EndMs {
				idx := int((rec.At - plan.StartMs) / plan.BucketWidthMs)
				if idx >= 0 && idx < plan.BucketCount {
					timelineModelSets[idx][rec.Model] = struct{}{}
				}
				mStat := modelsMap[rec.Model]
				if mStat == nil {
					mStat = &ModelStat{Name: rec.Model}
					modelsMap[rec.Model] = mStat
				}
				mStat.Count++
				mStat.Tokens += rec.TotalTokens
				mStat.PromptTokens += rec.PromptTokens
				mStat.CompletionTokens += rec.CompletionTokens
				mStat.ReasoningTokens += rec.ReasoningTokens

				if rec.AccountID != "" {
					aStat := accountsMap[rec.AccountID]
					if aStat == nil {
						aStat = &EntityStat{ID: rec.AccountID}
						accountsMap[rec.AccountID] = aStat
					}
					aStat.Count++
					aStat.Tokens += rec.TotalTokens
				}
				if rec.CallerID != "" {
					cStat := callersMap[rec.CallerID]
					if cStat == nil {
						cStat = &EntityStat{ID: rec.CallerID}
						callersMap[rec.CallerID] = cStat
					}
					cStat.Count++
					cStat.Tokens += rec.TotalTokens
				}
			}
		}
	}

	// Finalize timeline buckets
	for i := 0; i < plan.BucketCount; i++ {
		tb := &timeline[i]
		if tb.elapsedCount > 0 {
			tb.AvgLatencyMs = int64(math.Round(float64(tb.elapsedMs) / float64(tb.elapsedCount)))
		}
		for m := range timelineModelSets[i] {
			tb.Models = append(tb.Models, m)
		}
		sort.Strings(tb.Models)
	}

	// Finalize totals
	totalsResp := calculateTotalsResponse(queryTotals)

	// Finalize models, accounts, callers TopN
	modelsList := make([]ModelStat, 0, len(modelsMap))
	for _, m := range modelsMap {
		modelsList = append(modelsList, *m)
	}
	modelsList = collapseModelsTopN(modelsList, top, totalsResp.TotalTokens)

	accountsList := make([]EntityStat, 0, len(accountsMap))
	for _, a := range accountsMap {
		accountsList = append(accountsList, *a)
	}
	accountsList = collapseEntitiesTopN(accountsList, top, totalsResp.TotalTokens)

	callersList := make([]EntityStat, 0, len(callersMap))
	for _, c := range callersMap {
		callersList = append(callersList, *c)
	}
	callersList = collapseEntitiesTopN(callersList, top, totalsResp.TotalTokens)

	// Filter recent list
	recentSlice := make([]Record, 0, recentLimit)
	for _, r := range s.recent {
		if len(recentSlice) >= recentLimit {
			break
		}
		recentSlice = append(recentSlice, *r)
	}

	return QueryResult{
		Version:   1,
		Revision:  s.revision,
		FlushedAt: s.flushedAt,
		Path:      s.path,
		Backfill:  s.backfill,
		Range: RangeInfo{
			Key:           plan.RangeKey,
			StartMs:       plan.StartMs,
			EndMs:         plan.EndMs,
			Tier:          plan.Tier,
			BucketWidthMs: plan.BucketWidthMs,
			BucketCount:   plan.BucketCount,
			Truncated:     plan.Truncated,
		},
		Totals:   totalsResp,
		Models:   modelsList,
		Accounts: accountsList,
		Callers:  callersList,
		Timeline: timeline,
		Recent:   recentSlice,
	}, nil
}

func calculateTotalsResponse(c Counters) TotalsResponse {
	var avgLatency int64
	if c.ElapsedCount > 0 {
		avgLatency = int64(math.Round(float64(c.ElapsedMs) / float64(c.ElapsedCount)))
	}

	successRate := 100
	if c.Requests > 0 {
		successRate = int(math.Round(float64(c.Success) / float64(c.Requests) * 100))
	}

	var tokensPerSec float64
	var tokensPerMin int64
	if c.Requests > 0 && c.LastMs > c.FirstMs {
		spanSec := math.Max(1, math.Round(float64(c.LastMs-c.FirstMs)/1000.0))
		tokensPerSec = math.Round(float64(c.TotalTokens)/spanSec*10) / 10
		tokensPerMin = int64(math.Round(tokensPerSec * 60))
	}

	return TotalsResponse{
		Requests:         c.Requests,
		Success:          c.Success,
		Errors:           c.Errors,
		Stopped:          c.Stopped,
		PromptTokens:     c.PromptTokens,
		CompletionTokens: c.CompletionTokens,
		ReasoningTokens:  c.ReasoningTokens,
		TotalTokens:      c.TotalTokens,
		AvgLatencyMs:     avgLatency,
		SuccessRate:      successRate,
		TokensPerSec:     tokensPerSec,
		TokensPerMin:     tokensPerMin,
		FirstMs:          c.FirstMs,
		LastMs:           c.LastMs,
	}
}

func collapseModelsTopN(items []ModelStat, top int, totalTokens int64) []ModelStat {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tokens == items[j].Tokens {
			return items[i].Count > items[j].Count
		}
		return items[i].Tokens > items[j].Tokens
	})
	var result []ModelStat
	if len(items) <= top {
		result = items
	} else {
		result = make([]ModelStat, 0, top)
		result = append(result, items[:top-1]...)
		other := ModelStat{Name: "__other__"}
		for _, it := range items[top-1:] {
			other.Count += it.Count
			other.Tokens += it.Tokens
			other.PromptTokens += it.PromptTokens
			other.CompletionTokens += it.CompletionTokens
			other.ReasoningTokens += it.ReasoningTokens
		}
		result = append(result, other)
	}
	for i := range result {
		if totalTokens > 0 {
			result[i].Percentage = int(math.Round(float64(result[i].Tokens) / float64(totalTokens) * 100))
		}
	}
	return result
}

func collapseEntitiesTopN(items []EntityStat, top int, totalTokens int64) []EntityStat {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tokens == items[j].Tokens {
			return items[i].Count > items[j].Count
		}
		return items[i].Tokens > items[j].Tokens
	})
	var result []EntityStat
	if len(items) <= top {
		result = items
	} else {
		result = make([]EntityStat, 0, top)
		result = append(result, items[:top-1]...)
		other := EntityStat{ID: "__other__"}
		for _, it := range items[top-1:] {
			other.Count += it.Count
			other.Tokens += it.Tokens
		}
		result = append(result, other)
	}
	for i := range result {
		if totalTokens > 0 {
			result[i].Percentage = int(math.Round(float64(result[i].Tokens) / float64(totalTokens) * 100))
		}
	}
	return result
}

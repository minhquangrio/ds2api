package usageledger

import (
	"math"
	"time"
)

type Tier string

const (
	TierMinute Tier = "minute"
	TierHour   Tier = "hour"
	TierDay    Tier = "day"
)

const (
	MinuteWidthMs  = 60 * 1000               // 60s = 60,000 ms
	FiveMinWidthMs = 5 * 60 * 1000           // 5m = 300,000 ms
	HourWidthMs    = 60 * 60 * 1000          // 1h = 3,600,000 ms
	DayWidthMs     = 24 * 60 * 60 * 1000     // 24h = 86,400,000 ms
	TwoDaysWidthMs = 2 * 24 * 60 * 60 * 1000 // 2d = 172,800,000 ms

	MinuteRetentionMs = int64(6 * time.Hour / time.Millisecond)            // 6h = 21,600,000 ms
	HourRetentionMs   = int64(45 * 24 * time.Hour / time.Millisecond)      // 45d = 3,888,000,000 ms
	DayRetentionMs    = int64(5 * 365 * 24 * time.Hour / time.Millisecond) // 5y = 157,680,000,000 ms
)

func pickTier(startMs, now int64) Tier {
	diff := now - startMs
	if diff <= MinuteRetentionMs {
		return TierMinute
	}
	if diff <= HourRetentionMs {
		return TierHour
	}
	return TierDay
}

type BucketPlan struct {
	RangeKey      string
	Tier          Tier
	BucketWidthMs int64
	BucketCount   int
	StartMs       int64
	EndMs         int64
	Truncated     bool
}

func PlanRange(rangeKey string, startMs, endMs, nowMs, firstMs int64) BucketPlan {
	switch rangeKey {
	case "15m":
		return BucketPlan{
			RangeKey:      "15m",
			Tier:          TierMinute,
			BucketWidthMs: MinuteWidthMs,
			BucketCount:   15,
			StartMs:       nowMs - 15*MinuteWidthMs,
			EndMs:         nowMs,
			Truncated:     false,
		}
	case "1h":
		return BucketPlan{
			RangeKey:      "1h",
			Tier:          TierMinute,
			BucketWidthMs: FiveMinWidthMs,
			BucketCount:   12,
			StartMs:       nowMs - 60*MinuteWidthMs,
			EndMs:         nowMs,
			Truncated:     false,
		}
	case "24h":
		return BucketPlan{
			RangeKey:      "24h",
			Tier:          TierHour,
			BucketWidthMs: HourWidthMs,
			BucketCount:   24,
			StartMs:       nowMs - 24*HourWidthMs,
			EndMs:         nowMs,
			Truncated:     false,
		}
	case "7d":
		return BucketPlan{
			RangeKey:      "7d",
			Tier:          TierHour,
			BucketWidthMs: DayWidthMs,
			BucketCount:   7,
			StartMs:       nowMs - 7*DayWidthMs,
			EndMs:         nowMs,
			Truncated:     false,
		}
	case "30d":
		return BucketPlan{
			RangeKey:      "30d",
			Tier:          TierHour,
			BucketWidthMs: TwoDaysWidthMs,
			BucketCount:   15,
			StartMs:       nowMs - 30*DayWidthMs,
			EndMs:         nowMs,
			Truncated:     false,
		}
	case "all":
		minTs := firstMs
		if minTs == 0 || minTs > nowMs {
			minTs = nowMs - 3600*1000
		}
		span := nowMs - minTs
		if span < 60*1000 {
			span = 60 * 1000
		}
		n := int(math.Min(20, math.Max(8, math.Round(float64(span)/60000.0))))
		width := int64(math.Ceil(float64(span) / float64(n)))
		tier := pickTier(minTs, nowMs)
		truncated := (nowMs - minTs) > DayRetentionMs
		return BucketPlan{
			RangeKey:      "all",
			Tier:          tier,
			BucketWidthMs: width,
			BucketCount:   n,
			StartMs:       minTs,
			EndMs:         nowMs,
			Truncated:     truncated,
		}
	case "custom":
		span := endMs - startMs
		if span < 60*1000 {
			span = 60 * 1000
		}
		n := int(math.Min(20, math.Max(8, math.Round(float64(span)/60000.0))))
		width := int64(math.Ceil(float64(span) / float64(n)))
		tier := pickTier(startMs, nowMs)
		truncated := (nowMs - startMs) > DayRetentionMs
		return BucketPlan{
			RangeKey:      "custom",
			Tier:          tier,
			BucketWidthMs: width,
			BucketCount:   n,
			StartMs:       startMs,
			EndMs:         endMs,
			Truncated:     truncated,
		}
	default:
		return BucketPlan{}
	}
}

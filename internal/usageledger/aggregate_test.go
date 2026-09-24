package usageledger

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBucketPlanJSScaleComparison(t *testing.T) {
	now := int64(1700000000000)

	cases := []struct {
		rangeKey      string
		expectedWidth int64
		expectedCount int
		expectedTier  Tier
	}{
		{"15m", 60000, 15, TierMinute},
		{"1h", 300000, 12, TierMinute},
		{"24h", 3600000, 24, TierHour},
		{"7d", 86400000, 7, TierHour},
		{"30d", 172800000, 15, TierHour},
	}

	for _, tc := range cases {
		t.Run(tc.rangeKey, func(t *testing.T) {
			plan := PlanRange(tc.rangeKey, 0, 0, now, 0)
			if plan.BucketWidthMs != tc.expectedWidth {
				t.Fatalf("expected width %d, got %d", tc.expectedWidth, plan.BucketWidthMs)
			}
			if plan.BucketCount != tc.expectedCount {
				t.Fatalf("expected count %d, got %d", tc.expectedCount, plan.BucketCount)
			}
			if plan.Tier != tc.expectedTier {
				t.Fatalf("expected tier %s, got %s", tc.expectedTier, plan.Tier)
			}
		})
	}
}

func TestPickTierBoundaries(t *testing.T) {
	now := time.Now().UnixMilli()

	// Exactly at 6h boundary
	t6h := now - MinuteRetentionMs
	if pickTier(t6h, now) != TierMinute {
		t.Fatalf("expected TierMinute at 6h boundary")
	}
	// 1ms beyond 6h
	if pickTier(t6h-1, now) != TierHour {
		t.Fatalf("expected TierHour just past 6h boundary")
	}

	// Exactly at 45d boundary
	t45d := now - HourRetentionMs
	if pickTier(t45d, now) != TierHour {
		t.Fatalf("expected TierHour at 45d boundary")
	}
	// 1ms beyond 45d
	if pickTier(t45d-1, now) != TierDay {
		t.Fatalf("expected TierDay just past 45d boundary")
	}
}

func TestCustomRangeSpanningTiersServedFromDayTier(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "usage_ledger.json")
	store := New(storePath)
	defer func() {
		_ = store.Close()
	}()

	now := time.Now().UnixMilli()
	// Record 3 events across 60 days
	t1 := now - 60*24*3600*1000 // 60 days ago
	t2 := now - 30*24*3600*1000 // 30 days ago
	t3 := now - 2*3600*1000     // 2 hours ago

	store.mu.Lock()
	store.recordLocked(Record{
		ID: "r1", Model: "m1", At: t1, Status: "success", StatusCode: 200, TotalTokens: 100, ElapsedMs: 100,
	})
	store.recordLocked(Record{
		ID: "r2", Model: "m2", At: t2, Status: "success", StatusCode: 200, TotalTokens: 200, ElapsedMs: 200,
	})
	store.recordLocked(Record{
		ID: "r3", Model: "m1", At: t3, Status: "error", StatusCode: 500, TotalTokens: 50, ElapsedMs: 300,
	})
	store.mu.Unlock()

	// Query custom range covering t1 to now (spans across minute/hour into day tier)
	res, err := store.Query(QueryOptions{
		RangeKey: "custom",
		StartMs:  t1 - 1000,
		EndMs:    now + 1000,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if res.Range.Tier != TierDay {
		t.Fatalf("expected TierDay for range spanning 60 days, got %s", res.Range.Tier)
	}
	if res.Range.Truncated {
		t.Fatalf("expected truncated: false, got true")
	}
	if res.Totals.TotalTokens != 350 {
		t.Fatalf("expected total tokens 350, got %d", res.Totals.TotalTokens)
	}
	if res.Totals.Requests != 3 {
		t.Fatalf("expected 3 requests, got %d", res.Totals.Requests)
	}
	if res.Totals.Success != 2 || res.Totals.Errors != 1 {
		t.Fatalf("expected 2 success and 1 error, got %+v", res.Totals)
	}

	// Success rate = success / requests = 2 / 3 = 67%
	if res.Totals.SuccessRate != 67 {
		t.Fatalf("expected success rate 67, got %d", res.Totals.SuccessRate)
	}

	// Weighted average latency: (100 + 200 + 300) / 3 = 200 ms
	if res.Totals.AvgLatencyMs != 200 {
		t.Fatalf("expected avg latency 200, got %d", res.Totals.AvgLatencyMs)
	}
}

func TestTopNCollapsesIntoOtherAndRoundsPercentage(t *testing.T) {
	models := []ModelStat{
		{Name: "m1", Tokens: 60, Count: 6},
		{Name: "m2", Tokens: 25, Count: 2},
		{Name: "m3", Tokens: 10, Count: 1},
		{Name: "m4", Tokens: 5, Count: 1},
	}
	totalTokens := int64(100)

	// Keep top 3, m4 should be collapsed into __other__
	collapsed := collapseModelsTopN(models, 3, totalTokens)
	if len(collapsed) != 3 {
		t.Fatalf("expected 3 items, got %d", len(collapsed))
	}
	if collapsed[0].Name != "m1" || collapsed[0].Percentage != 60 {
		t.Fatalf("unexpected item 0: %+v", collapsed[0])
	}
	if collapsed[1].Name != "m2" || collapsed[1].Percentage != 25 {
		t.Fatalf("unexpected item 1: %+v", collapsed[1])
	}
	if collapsed[2].Name != "__other__" || collapsed[2].Tokens != 15 || collapsed[2].Percentage != 15 {
		t.Fatalf("unexpected other item: %+v", collapsed[2])
	}
}

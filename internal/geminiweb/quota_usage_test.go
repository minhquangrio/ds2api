package geminiweb

import (
	"testing"
)

func TestParseUsageInfoResponse(t *testing.T) {
	// Sample batchexecute response for jSf9Qc with Tier 2 (PRO), 5h window (type 1), weekly (type 2), ai credits (type 3)
	sample := `)[}'
250
[["wrb.fr","jSf9Qc","[2,[[45,0.1,1,[[1741234567]]],[200,0.25,2,[[1741839000]]],[15,0.0,3]],true]",null,null,null,"generic"]]
`
	usage := ParseUsageInfoResponse(sample)
	if usage == nil {
		t.Fatalf("expected non-nil UsageInfo")
	}

	if usage.Tier.ID != 2 || usage.Tier.Label != "PRO" {
		t.Errorf("expected Tier 2 PRO, got ID=%d Label=%s", usage.Tier.ID, usage.Tier.Label)
	}
	if usage.UseOverageAICredits != true {
		t.Errorf("expected UseOverageAICredits true, got %v", usage.UseOverageAICredits)
	}

	// Check 5h metric
	if usage.Current5h == nil {
		t.Fatalf("expected Current5h to be populated")
	}
	if usage.Current5h.Window != "5h" {
		t.Errorf("expected 5h window, got %s", usage.Current5h.Window)
	}
	if usage.Current5h.RemainingCredits == nil || *usage.Current5h.RemainingCredits != 45 {
		t.Errorf("expected 45 remaining credits, got %v", usage.Current5h.RemainingCredits)
	}
	if usage.Current5h.UsagePercentage == nil || *usage.Current5h.UsagePercentage != 10 {
		t.Errorf("expected 10%% usage, got %v", usage.Current5h.UsagePercentage)
	}
	if usage.Current5h.ResetTimestamp != 1741234567 {
		t.Errorf("expected reset timestamp 1741234567, got %d", usage.Current5h.ResetTimestamp)
	}
	if usage.Current5h.ResetAt == "" {
		t.Errorf("expected non-empty ResetAt")
	}

	// Check weekly metric
	if usage.Weekly == nil {
		t.Fatalf("expected Weekly to be populated")
	}
	if usage.Weekly.Window != "weekly" {
		t.Errorf("expected weekly window, got %s", usage.Weekly.Window)
	}
	if usage.Weekly.RemainingCredits == nil || *usage.Weekly.RemainingCredits != 200 {
		t.Errorf("expected 200 weekly credits, got %v", usage.Weekly.RemainingCredits)
	}
	if usage.Weekly.UsagePercentage == nil || *usage.Weekly.UsagePercentage != 25 {
		t.Errorf("expected 25%% weekly usage, got %v", usage.Weekly.UsagePercentage)
	}

	// Check AI credits remaining (metric type 3)
	if usage.AICreditsRemaining == nil || *usage.AICreditsRemaining != 15 {
		t.Errorf("expected 15 AI credits remaining, got %v", usage.AICreditsRemaining)
	}
}

func TestParseUsageInfoTier6Ultra(t *testing.T) {
	sample := `)[}'
120
[["wrb.fr","jSf9Qc","[6,[[100,0.0,1,[[1741234567]]]],null]",null,null,null,"generic"]]
`
	usage := ParseUsageInfoResponse(sample)
	if usage == nil {
		t.Fatalf("expected non-nil UsageInfo")
	}
	if usage.Tier.ID != 6 || usage.Tier.Label != "ULTRA" {
		t.Errorf("expected Tier 6 ULTRA, got ID=%d Label=%s", usage.Tier.ID, usage.Tier.Label)
	}
}

func TestParseExtraQuotaResponse(t *testing.T) {
	// Blocked extra features with reset timestamp
	sampleBlocked := `)[}'
120
[["wrb.fr","aPya6c","[true,1.0,[1741239999]]",null,null,null,"generic"]]
`
	extra := ParseExtraQuotaResponse(sampleBlocked)
	if extra == nil {
		t.Fatalf("expected non-nil ExtraQuotaInfo")
	}
	if !extra.IsBlocked {
		t.Errorf("expected IsBlocked true")
	}
	if extra.UsagePercentage == nil || *extra.UsagePercentage != 100.0 {
		t.Errorf("expected 100%% usage percentage, got %v", extra.UsagePercentage)
	}
	if extra.ResetTime != 1741239999 {
		t.Errorf("expected ResetTime 1741239999, got %d", extra.ResetTime)
	}

	// Non-blocked extra features
	sampleOk := `)[}'
120
[["wrb.fr","aPya6c","[false,0.15,[1741238888]]",null,null,null,"generic"]]
`
	extraOk := ParseExtraQuotaResponse(sampleOk)
	if extraOk == nil {
		t.Fatalf("expected non-nil ExtraQuotaInfo")
	}
	if extraOk.IsBlocked {
		t.Errorf("expected IsBlocked false")
	}
	if extraOk.UsagePercentage == nil || *extraOk.UsagePercentage != 15.0 {
		t.Errorf("expected 15%% usage percentage, got %v", extraOk.UsagePercentage)
	}
}

func TestParseQuotaUnlimited(t *testing.T) {
	// Sample where total == 0 and remaining == 0 (Unlimited)
	sample := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"1\",11],1,0.0,[1741234567],0,0]]]",null,null,null,"generic"]]
`
	quotas := ParseQuotaResponse(sample)
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota, got %d", len(quotas))
	}
	q := quotas[QuotaActionFlash]
	if !q.IsUnlimited {
		t.Errorf("expected IsUnlimited to be true for 0/0 quota")
	}
	if q.Remaining != 0 || q.Total != 0 {
		t.Errorf("expected 0/0 remaining/total, got %d/%d", q.Remaining, q.Total)
	}
}

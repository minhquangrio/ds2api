package geminiweb

import (
	"testing"
)

func TestParseQuotaResponse(t *testing.T) {
	sample := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"1\",4],1,0.25,[1741234567],100,75],[[\"1\",11],1,0.1,[1741234567],200,180],[[\"1\",15],1,0.0,[1741234567],50,50]]]",null,null,null,"generic"]]
`
	quotas := ParseQuotaResponse(sample)
	if len(quotas) != 3 {
		t.Fatalf("expected 3 quotas parsed, got %d", len(quotas))
	}

	proQuota, ok := quotas[QuotaActionPro]
	if !ok {
		t.Fatalf("missing pro quota")
	}
	if proQuota.Remaining != 75 || proQuota.Total != 100 {
		t.Errorf("pro quota mismatch: remaining=%d total=%d", proQuota.Remaining, proQuota.Total)
	}
	if proQuota.Label != "Gemini Pro" {
		t.Errorf("pro quota label mismatch: got %s", proQuota.Label)
	}

	if proQuota.UsagePercentage != 25.0 {
		t.Errorf("pro quota usage percentage mismatch: expected 25.0, got %f", proQuota.UsagePercentage)
	}
	if proQuota.IsUnlimited {
		t.Errorf("expected pro quota to not be unlimited")
	}

	flashQuota, ok := quotas[QuotaActionFlash]
	if !ok {
		t.Fatalf("missing flash quota")
	}
	if flashQuota.Remaining != 180 || flashQuota.Total != 200 {
		t.Errorf("flash quota mismatch: remaining=%d total=%d", flashQuota.Remaining, flashQuota.Total)
	}
	if flashQuota.UsagePercentage != 10.0 {
		t.Errorf("flash quota usage percentage mismatch: expected 10.0, got %f", flashQuota.UsagePercentage)
	}

	thinkingQuota, ok := quotas[QuotaActionFlashThinking]
	if !ok {
		t.Fatalf("missing thinking quota")
	}
	if thinkingQuota.Remaining != 50 || thinkingQuota.Total != 50 {
		t.Errorf("thinking quota mismatch: remaining=%d total=%d", thinkingQuota.Remaining, thinkingQuota.Total)
	}
	if thinkingQuota.UsagePercentage != 0.0 {
		t.Errorf("thinking quota usage percentage mismatch: expected 0.0, got %f", thinkingQuota.UsagePercentage)
	}
}

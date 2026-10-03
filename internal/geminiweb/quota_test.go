package geminiweb

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestParseQuotaResponse(t *testing.T) {
	sample := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"1\",4],1,0.25,[1741234567],100,75],[[\"1\",11],1,0.1,[1741234567],200,180],[[\"1\",15],1,0.0,[1741234567],50,50]]]",null,null,null,"generic"]]
`
	quotas := ParseQuotaResponse(sample, "Pro")
	if len(quotas) != 3 {
		t.Fatalf("expected 3 quotas parsed, got %d", len(quotas))
	}

	proQuota, ok := quotas["1-4"]
	if !ok {
		t.Fatalf("missing pro quota for 1-4")
	}
	if proQuota.Remaining != 75 || proQuota.Total != 100 {
		t.Errorf("pro quota mismatch: remaining=%d total=%d", proQuota.Remaining, proQuota.Total)
	}
	if proQuota.Label != "Gemini Pro [1-4]" {
		t.Errorf("pro quota label mismatch: got %s, want Gemini Pro [1-4]", proQuota.Label)
	}
	if proQuota.QuotaID != "1-4" {
		t.Errorf("pro quota quota_id mismatch: got %s", proQuota.QuotaID)
	}
	if proQuota.ActionID != 4 {
		t.Errorf("pro quota action_id mismatch: got %d", proQuota.ActionID)
	}
	if proQuota.UsagePercentage != 25.0 {
		t.Errorf("pro quota usage percentage mismatch: expected 25.0, got %f", proQuota.UsagePercentage)
	}
	if proQuota.UsageLevel != 0.25 {
		t.Errorf("pro quota usage level mismatch: expected 0.25, got %f", proQuota.UsageLevel)
	}
	if proQuota.IsUnlimited {
		t.Errorf("expected pro quota to not be unlimited")
	}

	flashQuota, ok := quotas["1-11"]
	if !ok {
		t.Fatalf("missing flash quota for 1-11")
	}
	if flashQuota.Remaining != 180 || flashQuota.Total != 200 {
		t.Errorf("flash quota mismatch: remaining=%d total=%d", flashQuota.Remaining, flashQuota.Total)
	}
	if flashQuota.UsagePercentage != 10.0 {
		t.Errorf("flash quota usage percentage mismatch: expected 10.0, got %f", flashQuota.UsagePercentage)
	}
	if flashQuota.UsageLevel != 0.1 {
		t.Errorf("flash quota usage level mismatch: expected 0.1, got %f", flashQuota.UsageLevel)
	}
	if flashQuota.Label != "Gemini Flash [1-11]" {
		t.Errorf("flash quota label mismatch: got %s, want Gemini Flash [1-11]", flashQuota.Label)
	}

	thinkingQuota, ok := quotas["1-15"]
	if !ok {
		t.Fatalf("missing thinking quota for 1-15")
	}
	if thinkingQuota.Remaining != 50 || thinkingQuota.Total != 50 {
		t.Errorf("thinking quota mismatch: remaining=%d total=%d", thinkingQuota.Remaining, thinkingQuota.Total)
	}
	if thinkingQuota.UsagePercentage != 0.0 {
		t.Errorf("thinking quota usage percentage mismatch: expected 0.0, got %f", thinkingQuota.UsagePercentage)
	}
	if thinkingQuota.UsageLevel != 0.0 {
		t.Errorf("thinking quota usage level mismatch: expected 0.0, got %f", thinkingQuota.UsageLevel)
	}
	if thinkingQuota.Label != "Gemini Flash Thinking [1-15]" {
		t.Errorf("thinking quota label mismatch: got %s, want Gemini Flash Thinking [1-15]", thinkingQuota.Label)
	}
}

func TestParseQuotaResponse_RealWorldFlashKeys(t *testing.T) {
	sample := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"1\",11],1,0.0,[1741234567],100,100],[[\"2\",11],1,0.0,[1741234567],100,100],[[\"6\",11],1,0.0,[1741234567],100,100]]]",null,null,null,"generic"]]
`
	quotas := ParseQuotaResponse(sample, "Flash")
	if len(quotas) != 3 {
		t.Fatalf("expected 3 quotas parsed, got %d", len(quotas))
	}

	for _, key := range []string{"1-11", "2-11", "6-11"} {
		q, ok := quotas[key]
		if !ok {
			t.Errorf("expected quota with key %s to exist", key)
			continue
		}
		if q.ActionID != 11 {
			t.Errorf("expected ActionID 11 for key %s, got %d", key, q.ActionID)
		}
		if q.QuotaID != key {
			t.Errorf("expected QuotaID %s, got %s", key, q.QuotaID)
		}
		expectedLabel := fmt.Sprintf("Gemini Flash [%s]", key)
		if q.Label != expectedLabel {
			t.Errorf("expected label %s, got %s", expectedLabel, q.Label)
		}
	}
}

func TestParseQuotaResponse_Action6FallsBackToCategory(t *testing.T) {
	sample := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"6\",6],1,0.2,[1741234567],50,40]]]",null,null,null,"generic"]]
`
	quotas := ParseQuotaResponse(sample, "Pro")
	if len(quotas) != 1 {
		t.Fatalf("expected 1 quota parsed, got %d", len(quotas))
	}
	q, ok := quotas["6-6"]
	if !ok {
		t.Fatalf("expected quota for 6-6")
	}
	if q.ActionID != 6 {
		t.Errorf("expected ActionID 6, got %d", q.ActionID)
	}
	if q.Label != "Gemini Pro [6-6]" {
		t.Errorf("expected label 'Gemini Pro [6-6]', got %s", q.Label)
	}
}

func TestParseQuotaResponse_UsageFromTotalRemaining(t *testing.T) {
	// total 200, remaining 180 -> used 20/200 = 10%
	// Test case 1: raw = 0.1
	sample1 := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"1\",11],1,0.1,[1741234567],200,180]]]",null,null,null,"generic"]]
`
	quotas1 := ParseQuotaResponse(sample1, "Flash")
	q1 := quotas1["1-11"]
	if q1.UsagePercentage != 10.0 {
		t.Errorf("expected UsagePercentage 10.0, got %f", q1.UsagePercentage)
	}
	if q1.UsageLevel != 0.1 {
		t.Errorf("expected UsageLevel 0.1, got %f", q1.UsageLevel)
	}

	// Test case 2: raw = 10.0 (raw on a 0-100 scale), total/remaining still determines usage percentage
	sample2 := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"1\",11],1,10.0,[1741234567],200,180]]]",null,null,null,"generic"]]
`
	quotas2 := ParseQuotaResponse(sample2, "Flash")
	q2 := quotas2["1-11"]
	if q2.UsagePercentage != 10.0 {
		t.Errorf("expected UsagePercentage 10.0, got %f", q2.UsagePercentage)
	}
	if q2.UsageLevel != 10.0 {
		t.Errorf("expected UsageLevel 10.0, got %f", q2.UsageLevel)
	}
}

func TestParseQuotaResponse_IDListStringAndNumber(t *testing.T) {
	sampleStr := `)[}'
120
[["wrb.fr","qpEbW","[[[[\"1\",11],1,0.0,[1741234567],100,100]]]",null,null,null,"generic"]]
`
	quotasStr := ParseQuotaResponse(sampleStr, "Flash")
	if _, ok := quotasStr["1-11"]; !ok {
		t.Errorf("expected key '1-11' for string idList")
	}

	sampleNum := `)[}'
120
[["wrb.fr","qpEbW","[[[[1,11],1,0.0,[1741234567],100,100]]]",null,null,null,"generic"]]
`
	quotasNum := ParseQuotaResponse(sampleNum, "Flash")
	if _, ok := quotasNum["1-11"]; !ok {
		t.Errorf("expected key '1-11' for numeric idList")
	}
}

func TestParseQuotaResponse_MissingIDList(t *testing.T) {
	// item[0] is empty or not a valid list
	sample := `)[}'
120
[["wrb.fr","qpEbW","[[[[],1,0.0,[1741234567],100,100],[[1],1,0.0,[1741234567],100,100]]]",null,null,null,"generic"]]
`
	quotas := ParseQuotaResponse(sample, "Flash")
	if len(quotas) != 0 {
		t.Errorf("expected 0 quotas parsed from missing idList, got %d", len(quotas))
	}
	if _, ok := quotas["-"]; ok {
		t.Errorf("should not contain '-' key")
	}
}

func TestParseQuotaResponse_Garbage(t *testing.T) {
	samples := []string{
		"",
		"not json at all",
		`[["other.rpc","xyz"]`,
		`)[}'
10
[["wrb.fr","otherRPC","[]",null,null,null,"generic"]]`,
	}
	for _, s := range samples {
		quotas := ParseQuotaResponse(s, "Flash")
		if len(quotas) != 0 {
			t.Errorf("expected 0 quotas from garbage input, got %d", len(quotas))
		}
	}
}

func TestCollectQuotaPayloads_AllFail(t *testing.T) {
	payloads := []quotaPayload{
		{Payload: "flash_payload", Category: "Flash"},
		{Payload: "pro_payload", Category: "Pro"},
	}
	fakeFetch := func(ctx context.Context, session SessionParams, payload, category string) (map[string]QuotaInfo, error) {
		return nil, fmt.Errorf("connection timeout")
	}

	res, err := collectQuotaPayloads(context.Background(), SessionParams{}, payloads, fakeFetch)
	if res != nil {
		t.Errorf("expected nil result on all fail, got %v", res)
	}
	if err == nil {
		t.Fatalf("expected error on all fail, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "Flash") || !strings.Contains(errMsg, "Pro") {
		t.Errorf("expected error message to mention both Flash and Pro, got %s", errMsg)
	}
}

func TestCollectQuotaPayloads_PartialFail(t *testing.T) {
	payloads := []quotaPayload{
		{Payload: "flash_payload", Category: "Flash"},
		{Payload: "pro_payload", Category: "Pro"},
	}
	fakeFetch := func(ctx context.Context, session SessionParams, payload, category string) (map[string]QuotaInfo, error) {
		if category == "Flash" {
			return map[string]QuotaInfo{
				"1-11": {QuotaID: "1-11", ActionID: 11, Label: "Gemini Flash [1-11]"},
			}, nil
		}
		return nil, fmt.Errorf("http 401 unauthorized")
	}

	res, err := collectQuotaPayloads(context.Background(), SessionParams{}, payloads, fakeFetch)
	if res == nil || len(res) != 1 {
		t.Fatalf("expected 1 quota item in partial success, got %v", res)
	}
	if _, ok := res["1-11"]; !ok {
		t.Errorf("expected key 1-11 in result")
	}
	if err == nil {
		t.Fatalf("expected non-nil error indicating partial failure")
	}
	if !strings.Contains(err.Error(), "partial quota failure") || !strings.Contains(err.Error(), "Pro") {
		t.Errorf("expected partial quota failure mentioning Pro, got %s", err.Error())
	}
}

func TestCollectQuotaPayloads_MergesDistinctKeys(t *testing.T) {
	payloads := []quotaPayload{
		{Payload: "flash_payload", Category: "Flash"},
		{Payload: "pro_payload", Category: "Pro"},
	}
	fakeFetch := func(ctx context.Context, session SessionParams, payload, category string) (map[string]QuotaInfo, error) {
		if category == "Flash" {
			return map[string]QuotaInfo{
				"1-11": {QuotaID: "1-11", ActionID: 11},
			}, nil
		}
		return map[string]QuotaInfo{
			"1-4": {QuotaID: "1-4", ActionID: 4},
		}, nil
	}

	res, err := collectQuotaPayloads(context.Background(), SessionParams{}, payloads, fakeFetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 quotas merged, got %d", len(res))
	}
	if _, ok := res["1-11"]; !ok {
		t.Errorf("missing key 1-11")
	}
	if _, ok := res["1-4"]; !ok {
		t.Errorf("missing key 1-4")
	}
}

func TestCheckQuota_NoSession(t *testing.T) {
	client := &Client{}
	quotas, err := client.CheckQuota(context.Background())
	if quotas != nil {
		t.Errorf("expected nil quotas, got %v", quotas)
	}
	if err == nil {
		t.Fatalf("expected error when session is not initialized")
	}
	if !strings.Contains(err.Error(), "session not initialized") {
		t.Errorf("expected 'session not initialized', got %s", err.Error())
	}
}

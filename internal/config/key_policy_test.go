package config

import (
	"slices"
	"testing"
)

func TestAPIKeyPolicyBuildAndZero(t *testing.T) {
	cfg := Config{
		APIKeys: []APIKey{
			{
				Key:         "key-full",
				Accounts:    []string{"acc-1", "acc-2"},
				Models:      []string{"deepseek-chat", "DeepSeek-Reasoner"},
				QuotaTokens: 1000,
			},
			{
				Key: "key-empty",
			},
		},
	}
	store := &Store{cfg: cfg}
	store.rebuildIndexes()

	// Full policy
	p1 := store.APIKeyPolicy("key-full")
	if p1.QuotaTokens != 1000 {
		t.Errorf("expected QuotaTokens=1000, got %d", p1.QuotaTokens)
	}
	if len(p1.Accounts) != 2 {
		t.Errorf("expected 2 accounts, got %d", len(p1.Accounts))
	}
	if _, ok := p1.Accounts["acc-1"]; !ok {
		t.Errorf("expected acc-1 in policy")
	}
	if len(p1.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(p1.Models))
	}
	if _, ok := p1.Models["deepseek-chat"]; !ok {
		t.Errorf("expected deepseek-chat in policy")
	}
	if _, ok := p1.Models["deepseek-reasoner"]; !ok {
		t.Errorf("expected lowercase deepseek-reasoner in policy")
	}

	// Empty policy
	p2 := store.APIKeyPolicy("key-empty")
	if p2.QuotaTokens != 0 || len(p2.Accounts) != 0 || len(p2.Models) != 0 {
		t.Errorf("expected zero policy for key-empty, got %+v", p2)
	}

	// Unknown key
	p3 := store.APIKeyPolicy("non-existent")
	if p3.QuotaTokens != 0 || len(p3.Accounts) != 0 || len(p3.Models) != 0 {
		t.Errorf("expected zero policy for unknown key, got %+v", p3)
	}
}

func TestConfigCloneDeepCopyAPIKeySlices(t *testing.T) {
	orig := Config{
		APIKeys: []APIKey{
			{
				Key:      "k1",
				Accounts: []string{"a1", "a2"},
				Models:   []string{"m1"},
			},
		},
	}

	clone := orig.Clone()
	if !slices.Equal(orig.APIKeys[0].Accounts, clone.APIKeys[0].Accounts) {
		t.Fatalf("clone accounts mismatch")
	}

	// Mutate clone's slices
	clone.APIKeys[0].Accounts[0] = "mutated-account"
	clone.APIKeys[0].Models = append(clone.APIKeys[0].Models, "m2")

	// Ensure orig was NOT affected
	if orig.APIKeys[0].Accounts[0] == "mutated-account" {
		t.Errorf("aliasing detected: mutating clone.Accounts changed orig.Accounts")
	}
	if len(orig.APIKeys[0].Models) != 1 {
		t.Errorf("aliasing detected: appending to clone.Models affected orig.Models")
	}
}

func TestNormalizeAPIKeysTrimDedupe(t *testing.T) {
	input := []APIKey{
		{
			Key:          " k1 ",
			Name:         " name 1 ",
			Remark:       " remark 1 ",
			ToolsEnabled: true,
			Accounts:     []string{" acc1 ", "acc2", " acc1 ", ""},
			Models:       []string{" model1 ", "model2", "model1", " "},
			QuotaTokens:  500,
		},
		{
			Key: "k1", // duplicate key
		},
		{
			Key: "   ", // empty key
		},
	}

	norm := normalizeAPIKeys(input)
	if len(norm) != 1 {
		t.Fatalf("expected 1 normalized key, got %d", len(norm))
	}
	k := norm[0]
	if k.Key != "k1" || k.Name != "name 1" || k.Remark != "remark 1" || !k.ToolsEnabled {
		t.Errorf("unexpected key fields: %+v", k)
	}
	if !slices.Equal(k.Accounts, []string{"acc1", "acc2"}) {
		t.Errorf("expected trimmed/deduped accounts, got %#v", k.Accounts)
	}
	if !slices.Equal(k.Models, []string{"model1", "model2"}) {
		t.Errorf("expected trimmed/deduped models, got %#v", k.Models)
	}
	if k.QuotaTokens != 500 {
		t.Errorf("expected quota 500, got %d", k.QuotaTokens)
	}
}

func TestEqualAPIKeysDetectsPolicyChanges(t *testing.T) {
	base := []APIKey{
		{
			Key:         "k1",
			Accounts:    []string{"a1"},
			Models:      []string{"m1"},
			QuotaTokens: 100,
		},
	}

	// Identical
	c1 := []APIKey{
		{
			Key:         "k1",
			Accounts:    []string{"a1"},
			Models:      []string{"m1"},
			QuotaTokens: 100,
		},
	}
	if !equalAPIKeys(base, c1) {
		t.Errorf("expected equalAPIKeys to be true")
	}

	// Different Accounts
	c2 := []APIKey{
		{
			Key:         "k1",
			Accounts:    []string{"a1", "a2"},
			Models:      []string{"m1"},
			QuotaTokens: 100,
		},
	}
	if equalAPIKeys(base, c2) {
		t.Errorf("expected equalAPIKeys to be false on accounts diff")
	}

	// Different Models
	c3 := []APIKey{
		{
			Key:         "k1",
			Accounts:    []string{"a1"},
			Models:      []string{"m2"},
			QuotaTokens: 100,
		},
	}
	if equalAPIKeys(base, c3) {
		t.Errorf("expected equalAPIKeys to be false on models diff")
	}

	// Different Quota
	c4 := []APIKey{
		{
			Key:         "k1",
			Accounts:    []string{"a1"},
			Models:      []string{"m1"},
			QuotaTokens: 200,
		},
	}
	if equalAPIKeys(base, c4) {
		t.Errorf("expected equalAPIKeys to be false on quota diff")
	}
}

func TestReconcileCredentialsPreservesPolicyOnKeysOnlyChange(t *testing.T) {
	base := Config{
		Keys: []string{"k1", "k2"},
		APIKeys: []APIKey{
			{
				Key:         "k1",
				Name:        "key1",
				Accounts:    []string{"acc1"},
				Models:      []string{"deepseek-chat"},
				QuotaTokens: 5000,
			},
			{
				Key:         "k2",
				Name:        "key2",
				Accounts:    []string{"acc2"},
				Models:      []string{"deepseek-reasoner"},
				QuotaTokens: 10000,
			},
		},
	}

	// Only Keys changed: k2 removed, k3 added
	curr := base.Clone()
	curr.Keys = []string{"k1", "k3"}

	curr.ReconcileCredentials(base)

	if len(curr.APIKeys) != 2 {
		t.Fatalf("expected 2 APIKeys, got %d", len(curr.APIKeys))
	}
	// k1 should preserve full policy
	if curr.APIKeys[0].Key != "k1" || curr.APIKeys[0].QuotaTokens != 5000 || !slices.Equal(curr.APIKeys[0].Accounts, []string{"acc1"}) {
		t.Errorf("k1 policy was not preserved: %+v", curr.APIKeys[0])
	}
	// k3 is new, default empty policy
	if curr.APIKeys[1].Key != "k3" || curr.APIKeys[1].QuotaTokens != 0 || len(curr.APIKeys[1].Accounts) != 0 {
		t.Errorf("k3 unexpected policy: %+v", curr.APIKeys[1])
	}
}

func TestNormalizeAPIKeyAssignments(t *testing.T) {
	cfg := &Config{
		Accounts: []Account{
			{Email: "user1@example.com"},
			{Mobile: "+8613800000000"},
		},
		ModelAliases: map[string]string{
			"gpt-4.1": "deepseek-v4-flash",
		},
	}

	// 1. Happy path: alias canonicalized, account identifier resolved
	item := &APIKey{
		Key:         "k1",
		Accounts:    []string{"user1@example.com", "+8613800000000"},
		Models:      []string{"gpt-4.1", "deepseek-v4-pro"},
		QuotaTokens: 500,
	}
	if err := NormalizeAPIKeyAssignments(cfg, item); err != nil {
		t.Fatalf("unexpected error on happy path: %v", err)
	}
	if !slices.Equal(item.Accounts, []string{"user1@example.com", "+8613800000000"}) {
		t.Errorf("unexpected normalized accounts: %#v", item.Accounts)
	}
	if !slices.Equal(item.Models, []string{"deepseek-v4-flash", "deepseek-v4-pro"}) {
		t.Errorf("expected canonical models, got: %#v", item.Models)
	}

	// 2. Unknown account
	itemUnknownAcc := &APIKey{
		Accounts: []string{"unknown@example.com"},
	}
	if err := NormalizeAPIKeyAssignments(cfg, itemUnknownAcc); err == nil {
		t.Errorf("expected error for unknown account, got nil")
	}

	// 3. Unknown model
	itemUnknownModel := &APIKey{
		Models: []string{"unknown-model-xyz"},
	}
	if err := NormalizeAPIKeyAssignments(cfg, itemUnknownModel); err == nil {
		t.Errorf("expected error for unknown model, got nil")
	}

	// 4. Negative quota
	itemNegQuota := &APIKey{
		QuotaTokens: -10,
	}
	if err := NormalizeAPIKeyAssignments(cfg, itemNegQuota); err == nil {
		t.Errorf("expected error for negative quota, got nil")
	}

	// 5. Zero quota is valid (unlimited)
	itemZeroQuota := &APIKey{
		QuotaTokens: 0,
	}
	if err := NormalizeAPIKeyAssignments(cfg, itemZeroQuota); err != nil {
		t.Errorf("unexpected error for zero quota: %v", err)
	}
}

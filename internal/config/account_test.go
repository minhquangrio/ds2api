package config

import "testing"

func TestNormalizePoolType(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", PoolTypeDefault},
		{"default", PoolTypeDefault},
		{"DEFAULT", PoolTypeDefault},
		{"no_tools", PoolTypeNoTools},
		{"NO_TOOLS", PoolTypeNoTools},
		{"  no_tools  ", PoolTypeNoTools},
		{"tools_only", PoolTypeToolsOnly},
		{"TOOLS_ONLY", PoolTypeToolsOnly},
		{"unknown", PoolTypeDefault},
	}
	for _, c := range cases {
		if got := NormalizePoolType(c.in); got != c.want {
			t.Fatalf("NormalizePoolType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMatchesPoolType(t *testing.T) {
	defaultAcc := Account{PoolType: PoolTypeDefault}
	noToolsAcc := Account{PoolType: PoolTypeNoTools}
	toolsOnlyAcc := Account{PoolType: PoolTypeToolsOnly}
	emptyAcc := Account{}

	if !defaultAcc.MatchesPoolType(true) || !defaultAcc.MatchesPoolType(false) {
		t.Fatal("default account should match both tools-enabled and tools-disabled requests")
	}
	if !emptyAcc.MatchesPoolType(true) || !emptyAcc.MatchesPoolType(false) {
		t.Fatal("account with empty pool_type should match both tools-enabled and tools-disabled requests")
	}
	if noToolsAcc.MatchesPoolType(true) {
		t.Fatal("no_tools account should not match tools-enabled request")
	}
	if !noToolsAcc.MatchesPoolType(false) {
		t.Fatal("no_tools account should match tools-disabled request")
	}
	if toolsOnlyAcc.MatchesPoolType(false) {
		t.Fatal("tools_only account should not match tools-disabled request")
	}
	if !toolsOnlyAcc.MatchesPoolType(true) {
		t.Fatal("tools_only account should match tools-enabled request")
	}
}

func TestCleanWrappedToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"simple_token", "simple_token"},
		{"Bearer simple_token", "simple_token"},
		{`"quoted_token"`, "quoted_token"},
		{`{"value":"actual_token","__version":"0"}`, "actual_token"},
		{`{"token":"nested_token"}`, "nested_token"},
		{`{"userToken":"user_tok"}`, "user_tok"},
		{`{"value":"{\"value\":\"deep_token\"}"}`, "deep_token"},
	}
	for _, c := range cases {
		if got := CleanWrappedToken(c.in); got != c.want {
			t.Errorf("CleanWrappedToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

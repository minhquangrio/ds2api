package muse

import (
	"testing"
	"time"

	"ds2api/internal/config"
)

type mockStore struct {
	cfg config.Config
}

func (m *mockStore) Snapshot() config.Config {
	return m.cfg
}

func TestMuseCooldownDuration(t *testing.T) {
	t.Run("Default60s", func(t *testing.T) {
		t.Setenv("DS2API_MUSE_COOLDOWN_SECONDS", "")
		if d := museCooldownDuration(); d != 60*time.Second {
			t.Errorf("expected 60s, got %v", d)
		}
	})

	t.Run("CustomDuration", func(t *testing.T) {
		t.Setenv("DS2API_MUSE_COOLDOWN_SECONDS", "15")
		if d := museCooldownDuration(); d != 15*time.Second {
			t.Errorf("expected 15s, got %v", d)
		}
	})
}

func TestMuseRetryCeiling(t *testing.T) {
	store := &mockStore{
		cfg: config.Config{
			Accounts: []config.Account{
				{Provider: "muse", Name: "m1", Cookies: "sess1"},
				{Provider: "muse", Name: "m2", Cookies: "sess2"},
			},
		},
	}
	if ceiling := museRetryCeiling(store); ceiling != 2 {
		t.Errorf("expected ceiling 2, got %d", ceiling)
	}
}

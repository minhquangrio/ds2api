package codex

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestPKCEGeneration(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}

	if len(verifier) < 40 {
		t.Errorf("verifier too short: len = %d", len(verifier))
	}

	h := sha256.Sum256([]byte(verifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	if challenge != expectedChallenge {
		t.Errorf("challenge mismatch: got %s, want %s", challenge, expectedChallenge)
	}
}

func TestGenerateState(t *testing.T) {
	st1 := GenerateState()
	st2 := GenerateState()
	if st1 == "" || st2 == "" {
		t.Fatal("empty state generated")
	}
	if st1 == st2 {
		t.Fatal("subsequent states should be unique")
	}
}

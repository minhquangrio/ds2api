package config

import (
	"fmt"
	"strings"
)

type KeyPolicy struct {
	Accounts    map[string]struct{}
	Models      map[string]struct{}
	QuotaTokens int64
}

type modelAliasesMap map[string]string

func (m modelAliasesMap) ModelAliases() map[string]string { return m }

// NormalizeAPIKeyAssignments validates and normalizes an APIKey against cfg.
// Accounts must exist in cfg.Accounts (normalized to Identifier()).
// Models are resolved via ResolveModelTarget (normalized to target.Canonical).
// QuotaTokens must be >= 0.
func NormalizeAPIKeyAssignments(cfg *Config, item *APIKey) error {
	if cfg == nil || item == nil {
		return nil
	}
	if item.QuotaTokens < 0 {
		return fmt.Errorf("quota_tokens must be non-negative: %d", item.QuotaTokens)
	}

	if len(item.Accounts) > 0 {
		normalizedAccounts := make([]string, 0, len(item.Accounts))
		seenAccounts := make(map[string]struct{}, len(item.Accounts))
		for _, rawAcc := range item.Accounts {
			accName := strings.TrimSpace(rawAcc)
			if accName == "" {
				continue
			}
			var matchedID string
			for _, acc := range cfg.Accounts {
				if acc.Identifier() == accName || acc.Email == accName || (acc.Mobile != "" && CanonicalMobileKey(acc.Mobile) == CanonicalMobileKey(accName)) {
					matchedID = acc.Identifier()
					break
				}
			}
			if matchedID == "" {
				return fmt.Errorf("unknown account %q", accName)
			}
			if _, exists := seenAccounts[matchedID]; !exists {
				seenAccounts[matchedID] = struct{}{}
				normalizedAccounts = append(normalizedAccounts, matchedID)
			}
		}
		item.Accounts = normalizedAccounts
	}

	if len(item.Models) > 0 {
		normalizedModels := make([]string, 0, len(item.Models))
		seenModels := make(map[string]struct{}, len(item.Models))
		aliasReader := modelAliasesMap(cfg.ModelAliases)
		for _, rawModel := range item.Models {
			mName := strings.TrimSpace(rawModel)
			if mName == "" {
				continue
			}
			target, ok := ResolveModelTarget(aliasReader, mName)
			if !ok {
				return fmt.Errorf("unknown model %q", mName)
			}
			canonical := target.Canonical
			if canonical == "" {
				canonical = strings.ToLower(mName)
			}
			if _, exists := seenModels[canonical]; !exists {
				seenModels[canonical] = struct{}{}
				normalizedModels = append(normalizedModels, canonical)
			}
		}
		item.Models = normalizedModels
	}

	return nil
}

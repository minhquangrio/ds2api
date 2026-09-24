package configmgmt

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/config"
	"ds2api/internal/util"
)

func (h *Handler) updateConfig(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	old := h.Store.Snapshot()
	err := h.Store.Update(func(c *config.Config) error {
		if apiKeys, ok := toAPIKeys(req["api_keys"]); ok {
			c.APIKeys = apiKeys
		} else if keys, ok := toStringSlice(req["keys"]); ok {
			c.Keys = keys
		}
		if accountsRaw, ok := req["accounts"].([]any); ok {
			existing := map[string]config.Account{}
			for _, a := range old.Accounts {
				a = normalizeAccountForStorage(a)
				key := accountDedupeKey(a)
				if key != "" {
					existing[key] = a
				}
			}
			seen := map[string]struct{}{}
			accounts := make([]config.Account, 0, len(accountsRaw))
			for _, item := range accountsRaw {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				acc := normalizeAccountForStorage(toAccount(m))
				key := accountDedupeKey(acc)
				if key == "" {
					continue
				}
				if _, ok := seen[key]; ok {
					continue
				}
				if prev, ok := existing[key]; ok {
					if strings.TrimSpace(acc.Password) == "" {
						acc.Password = prev.Password
					}
				}
				seen[key] = struct{}{}
				accounts = append(accounts, acc)
			}
			c.Accounts = accounts
		}
		if m, ok := req["model_aliases"].(map[string]any); ok {
			aliases := make(map[string]string, len(m))
			for k, v := range m {
				aliases[k] = fmt.Sprintf("%v", v)
			}
			c.ModelAliases = aliases
		}
		for i := range c.APIKeys {
			if err := config.NormalizeAPIKeyAssignments(c, &c.APIKeys[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "配置已更新"})
}

func (h *Handler) addKey(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)
	key, _ := req["key"].(string)
	key = strings.TrimSpace(key)
	name := fieldString(req, "name")
	remark := fieldString(req, "remark")
	toolsEnabled := util.ToBool(req["tools_enabled"])
	accounts, _ := fieldStringSlice(req, "accounts")
	models, _ := fieldStringSlice(req, "models")
	quotaTokens, quotaOK := fieldInt64Optional(req, "quota_tokens")
	if quotaOK && quotaTokens < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "quota_tokens cannot be negative"})
		return
	}
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "Key 不能为空"})
		return
	}
	err := h.Store.Update(func(c *config.Config) error {
		for _, item := range c.APIKeys {
			if item.Key == key {
				return fmt.Errorf("key 已存在")
			}
		}
		newKey := config.APIKey{
			Key:          key,
			Name:         name,
			Remark:       remark,
			ToolsEnabled: toolsEnabled,
			Accounts:     accounts,
			Models:       models,
			QuotaTokens:  quotaTokens,
		}
		if err := config.NormalizeAPIKeyAssignments(c, &newKey); err != nil {
			return err
		}
		c.APIKeys = append(c.APIKeys, newKey)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_keys": len(h.Store.Snapshot().Keys)})
}

func (h *Handler) updateKey(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "key 不能为空"})
		return
	}

	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	name, nameOK := fieldStringOptional(req, "name")
	remark, remarkOK := fieldStringOptional(req, "remark")
	toolsEnabled, toolsEnabledOK := fieldBoolOptional(req, "tools_enabled")
	accounts, accountsOK := fieldStringSlice(req, "accounts")
	models, modelsOK := fieldStringSlice(req, "models")
	quotaTokens, quotaOK := fieldInt64Optional(req, "quota_tokens")
	if quotaOK && quotaTokens < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "quota_tokens cannot be negative"})
		return
	}

	err := h.Store.Update(func(c *config.Config) error {
		idx := -1
		for i, item := range c.APIKeys {
			if item.Key == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("key 不存在")
		}
		if nameOK {
			c.APIKeys[idx].Name = name
		}
		if remarkOK {
			c.APIKeys[idx].Remark = remark
		}
		if toolsEnabledOK {
			c.APIKeys[idx].ToolsEnabled = toolsEnabled
		}
		if accountsOK {
			c.APIKeys[idx].Accounts = accounts
		}
		if modelsOK {
			c.APIKeys[idx].Models = models
		}
		if quotaOK {
			c.APIKeys[idx].QuotaTokens = quotaTokens
		}
		if err := config.NormalizeAPIKeyAssignments(c, &c.APIKeys[idx]); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "不存在") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_keys": len(h.Store.Snapshot().Keys)})
}

func (h *Handler) deleteKey(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	err := h.Store.Update(func(c *config.Config) error {
		idx := -1
		for i, item := range c.APIKeys {
			if item.Key == key {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("key 不存在")
		}
		c.APIKeys = append(c.APIKeys[:idx], c.APIKeys[idx+1:]...)
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "total_keys": len(h.Store.Snapshot().Keys)})
}

func (h *Handler) batchImport(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "无效的 JSON 格式"})
		return
	}
	importedKeys, importedAccounts := 0, 0
	err := h.Store.Update(func(c *config.Config) error {
		if apiKeys, ok := toAPIKeys(req["api_keys"]); ok {
			var changed int
			c.APIKeys, changed = mergeAPIKeysPreferStructured(c.APIKeys, apiKeys)
			importedKeys += changed
		}
		if keys, ok := req["keys"].([]any); ok {
			legacy := make([]config.APIKey, 0, len(keys))
			for _, k := range keys {
				key := strings.TrimSpace(fmt.Sprintf("%v", k))
				if key == "" {
					continue
				}
				legacy = append(legacy, config.APIKey{Key: key})
			}
			var changed int
			c.APIKeys, changed = mergeAPIKeysPreferStructured(c.APIKeys, legacy)
			importedKeys += changed
		}
		if accounts, ok := req["accounts"].([]any); ok {
			existing := map[string]bool{}
			for _, a := range c.Accounts {
				a = normalizeAccountForStorage(a)
				key := accountDedupeKey(a)
				if key != "" {
					existing[key] = true
				}
			}
			for _, item := range accounts {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				acc := normalizeAccountForStorage(toAccount(m))
				key := accountDedupeKey(acc)
				if key == "" || existing[key] {
					continue
				}
				c.Accounts = append(c.Accounts, acc)
				existing[key] = true
				importedAccounts++
			}
		}
		return nil
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	h.Pool.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "imported_keys": importedKeys, "imported_accounts": importedAccounts})
}

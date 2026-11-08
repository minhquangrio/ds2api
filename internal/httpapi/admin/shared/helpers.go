package shared

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"ds2api/internal/config"
	"ds2api/internal/proxyrotate"
	"ds2api/internal/util"
)

var intFrom = util.IntFrom

var WriteJSON = util.WriteJSON
var IntFrom = util.IntFrom

func ReverseAccounts(a []config.Account) { reverseAccounts(a) }
func IntFromQuery(r *http.Request, key string, d int) int {
	return intFromQuery(r, key, d)
}
func NilIfEmpty(s string) any { return nilIfEmpty(s) }
func NilIfZero(v int64) any   { return nilIfZero(v) }
func MaskSecretPreview(secret string) string {
	return maskSecretPreview(secret)
}
func ToStringSlice(v any) ([]string, bool) { return toStringSlice(v) }
func ToAccount(m map[string]any) config.Account {
	return toAccount(m)
}
func ToAPIKeys(v any) ([]config.APIKey, bool) {
	return toAPIKeys(v)
}
func NormalizeAPIKeyForStorage(item config.APIKey) config.APIKey {
	return normalizeAPIKeyForStorage(item)
}
func APIKeyHasMetadata(item config.APIKey) bool {
	return apiKeyHasMetadata(item)
}
func MergeAPIKeysPreferStructured(existing, incoming []config.APIKey) ([]config.APIKey, int) {
	return mergeAPIKeysPreferStructured(existing, incoming)
}
func MergeAPIKeyRecord(existing, incoming config.APIKey) config.APIKey {
	return mergeAPIKeyRecord(existing, incoming)
}
func FieldString(m map[string]any, key string) string {
	return fieldString(m, key)
}
func FieldStringOptional(m map[string]any, key string) (string, bool) {
	return fieldStringOptional(m, key)
}
func FieldBoolOptional(m map[string]any, key string) (bool, bool) {
	return fieldBoolOptional(m, key)
}
func FieldStringSlice(m map[string]any, key string) ([]string, bool) {
	return fieldStringSlice(m, key)
}
func FieldInt64Optional(m map[string]any, key string) (int64, bool) {
	return fieldInt64Optional(m, key)
}
func StatusOr(v int, d int) int { return statusOr(v, d) }
func AccountMatchesIdentifier(acc config.Account, identifier string) bool {
	return accountMatchesIdentifier(acc, identifier)
}
func NormalizeAccountForStorage(acc config.Account) config.Account {
	return normalizeAccountForStorage(acc)
}
func ToProxy(m map[string]any) config.Proxy { return toProxy(m) }

func toProxy(m map[string]any) config.Proxy {
	proxy := config.Proxy{
		ID:       fieldString(m, "id"),
		Name:     fieldString(m, "name"),
		Type:     fieldString(m, "type"),
		Host:     fieldString(m, "host"),
		Port:     intFrom(m["port"]),
		Username: fieldString(m, "username"),
		Password: fieldString(m, "password"),
	}
	if raw, ok := m["rotate"]; ok {
		proxy.Rotate = ToRotateProxyConfig(raw)
	}
	if proxy.Rotate == nil {
		proxy.Rotate = toFlatRotateProxyConfig(m)
	}
	return config.NormalizeProxy(proxy)
}

// ToRotateProxyConfig builds a rotate block from a request value. It returns
// nil for null/empty payloads so callers can distinguish "clear rotation".
func ToRotateProxyConfig(v any) *config.RotateProxyConfig {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	rotate := &config.RotateProxyConfig{
		Provider:  fieldString(obj, "provider"),
		Endpoint:  fieldString(obj, "endpoint"),
		Key:       fieldString(obj, "key"),
		Network:   fieldString(obj, "network"),
		Province:  fieldString(obj, "province"),
		Protocol:  fieldString(obj, "protocol"),
		Whitelist: fieldString(obj, "whitelist"),
	}
	if ttl, ok := fieldInt64Optional(obj, "ttl_seconds"); ok {
		rotate.TTLSeconds = int(ttl)
	}
	if minRemaining, ok := fieldInt64Optional(obj, "min_remaining_seconds"); ok {
		rotate.MinRemainingSeconds = int(minRemaining)
	}
	if rotateConfigEmpty(rotate) {
		return nil
	}
	return rotate
}

// toFlatRotateProxyConfig accepts the flat `rotate_*` spelling so a curl call
// does not need a nested object.
func toFlatRotateProxyConfig(m map[string]any) *config.RotateProxyConfig {
	flat := map[string]any{}
	for target, source := range map[string]string{
		"provider":              "rotate_provider",
		"endpoint":              "rotate_endpoint",
		"key":                   "rotate_key",
		"network":               "rotate_network",
		"province":              "rotate_province",
		"protocol":              "rotate_protocol",
		"whitelist":             "rotate_whitelist",
		"ttl_seconds":           "rotate_ttl_seconds",
		"min_remaining_seconds": "rotate_min_remaining_seconds",
	} {
		if value, ok := m[source]; ok {
			flat[target] = value
		}
	}
	if len(flat) == 0 {
		return nil
	}
	return ToRotateProxyConfig(flat)
}

func rotateConfigEmpty(rotate *config.RotateProxyConfig) bool {
	if rotate == nil {
		return true
	}
	return strings.TrimSpace(rotate.Provider) == "" &&
		strings.TrimSpace(rotate.Endpoint) == "" &&
		strings.TrimSpace(rotate.Key) == "" &&
		strings.TrimSpace(rotate.Network) == "" &&
		strings.TrimSpace(rotate.Province) == "" &&
		strings.TrimSpace(rotate.Protocol) == "" &&
		strings.TrimSpace(rotate.Whitelist) == "" &&
		rotate.TTLSeconds == 0 &&
		rotate.MinRemainingSeconds == 0
}

// HasRotatePayload reports whether a request body mentioned rotation at all.
// Absent means "keep the stored block"; present-but-blank means "clear it".
func HasRotatePayload(m map[string]any) bool {
	if _, ok := m["rotate"]; ok {
		return true
	}
	for _, key := range []string{
		"rotate_provider", "rotate_endpoint", "rotate_key", "rotate_network",
		"rotate_province", "rotate_protocol", "rotate_whitelist",
		"rotate_ttl_seconds", "rotate_min_remaining_seconds",
	} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func FindProxyByID(c config.Config, proxyID string) (config.Proxy, bool) {
	return findProxyByID(c, proxyID)
}

// ProxyView is the redacted admin representation of one proxy entry. Secrets
// (password, rotating key) are never returned, only presence markers and a
// masked preview. A rotating entry reports its live lease so the UI can show the
// current exit node and how long it stays usable.
func ProxyView(proxy config.Proxy) map[string]any {
	proxy = config.NormalizeProxy(proxy)
	view := map[string]any{
		"id":           proxy.ID,
		"name":         proxy.Name,
		"type":         proxy.Type,
		"host":         proxy.Host,
		"port":         proxy.Port,
		"username":     proxy.Username,
		"has_password": strings.TrimSpace(proxy.Password) != "",
		"mode":         "static",
	}
	if proxy.Rotate == nil {
		return view
	}

	// The stored host/port of a rotating entry are placeholders; the address
	// in use comes from the lease, so hide the static fields entirely.
	view["mode"] = "rotate"
	view["host"] = ""
	view["port"] = 0
	view["username"] = ""
	view["has_password"] = false

	rotate := proxy.Rotate
	block := map[string]any{
		"provider":              rotate.Provider,
		"endpoint":              sanitizeEndpointPreview(rotate.Endpoint),
		"endpoint_host":         config.RotateEndpointHost(rotate.Endpoint),
		"has_key":               strings.TrimSpace(rotate.Key) != "",
		"key_preview":           MaskSecretPreview(rotate.Key),
		"network":               rotate.Network,
		"province":              rotate.Province,
		"protocol":              rotate.Protocol,
		"whitelist":             rotate.Whitelist,
		"ttl_seconds":           rotate.TTLSeconds,
		"min_remaining_seconds": rotate.MinRemainingSeconds,
	}
	if lease, ok := proxyrotate.Global().Status(proxy.ID); ok {
		block["lease"] = lease
	}
	view["rotate"] = block
	return view
}

// ProxyViews maps every configured proxy into its admin view.
func ProxyViews(proxies []config.Proxy) []map[string]any {
	items := make([]map[string]any, 0, len(proxies))
	for _, proxy := range proxies {
		items = append(items, ProxyView(proxy))
	}
	return items
}

// sanitizeEndpointPreview removes credentials and any embedded `key` query from
// a custom endpoint so the admin view can show the URL without leaking a secret.
func sanitizeEndpointPreview(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if parsed.User != nil {
		parsed.User = url.User("***")
	}
	query := parsed.Query()
	changed := false
	for name := range query {
		if strings.EqualFold(name, "key") || strings.EqualFold(name, "keyxoay") {
			query.Set(name, "***")
			changed = true
		}
	}
	if changed {
		parsed.RawQuery = query.Encode()
	} else if parsed.User == nil {
		return endpoint
	}
	return parsed.String()
}
func AccountDedupeKey(acc config.Account) string { return accountDedupeKey(acc) }
func NormalizeAndDedupeAccounts(accounts []config.Account) []config.Account {
	return normalizeAndDedupeAccounts(accounts)
}
func FindAccountByIdentifier(store ConfigStore, identifier string) (config.Account, bool) {
	return findAccountByIdentifier(store, identifier)
}

func ComputeSyncHash(store ConfigStore) string {
	if store == nil {
		return ""
	}
	snap := store.Snapshot().Clone()
	snap.ClearAccountTokens()
	snap.ClearVercelCredentials()
	snap.VercelSyncHash = ""
	snap.VercelSyncTime = 0
	b, _ := json.Marshal(snap)
	sum := md5.Sum(b)
	return fmt.Sprintf("%x", sum)
}

func SyncHashForJSON(s string) string {
	var cfg config.Config
	if err := json.Unmarshal([]byte(s), &cfg); err != nil {
		return ""
	}
	cfg.VercelSyncHash = ""
	cfg.VercelSyncTime = 0
	cfg.ClearAccountTokens()
	cfg.ClearVercelCredentials()
	b, err := json.Marshal(cfg)
	if err != nil {
		return ""
	}
	sum := md5.Sum(b)
	return fmt.Sprintf("%x", sum)
}

func reverseAccounts(a []config.Account) {
	for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
		a[i], a[j] = a[j], a[i]
	}
}

func intFromQuery(r *http.Request, key string, d int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nilIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func maskSecretPreview(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:2] + "****" + secret[len(secret)-2:]
}

func toStringSlice(v any) ([]string, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		out = append(out, strings.TrimSpace(fmt.Sprintf("%v", item)))
	}
	return out, true
}

func toAccount(m map[string]any) config.Account {
	email := fieldString(m, "email")
	mobile := config.NormalizeMobileForStorage(fieldString(m, "mobile"))
	provider := strings.ToLower(strings.TrimSpace(fieldString(m, "provider")))
	cookies := fieldString(m, "cookies")
	return config.Account{
		Name:     fieldString(m, "name"),
		Remark:   fieldString(m, "remark"),
		Email:    email,
		Mobile:   mobile,
		Password: fieldString(m, "password"),
		DeviceID: fieldString(m, "device_id"),
		ProxyID:  fieldString(m, "proxy_id"),
		PoolType: config.NormalizePoolType(fieldString(m, "pool_type")),
		Locale:   fieldString(m, "locale"),
		Provider: provider,
		Cookies:  cookies,
	}
}

func toAPIKeys(v any) ([]config.APIKey, bool) {
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]config.APIKey, 0, len(arr))
	seen := map[string]struct{}{}
	for _, item := range arr {
		switch x := item.(type) {
		case map[string]any:
			key := fieldString(x, "key")
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			accs, _ := fieldStringSlice(x, "accounts")
			mods, _ := fieldStringSlice(x, "models")
			quota, _ := fieldInt64Optional(x, "quota_tokens")
			out = append(out, config.APIKey{
				Key:          key,
				Name:         fieldString(x, "name"),
				Remark:       fieldString(x, "remark"),
				ToolsEnabled: util.ToBool(x["tools_enabled"]),
				Accounts:     accs,
				Models:       mods,
				QuotaTokens:  quota,
			})
		default:
			key := strings.TrimSpace(fmt.Sprintf("%v", item))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, config.APIKey{Key: key})
		}
	}
	return out, true
}

func normalizeAPIKeyForStorage(item config.APIKey) config.APIKey {
	var accounts []string
	if len(item.Accounts) > 0 {
		seen := make(map[string]struct{}, len(item.Accounts))
		for _, a := range item.Accounts {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if _, ok := seen[a]; !ok {
				seen[a] = struct{}{}
				accounts = append(accounts, a)
			}
		}
	}
	var models []string
	if len(item.Models) > 0 {
		seen := make(map[string]struct{}, len(item.Models))
		for _, m := range item.Models {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				models = append(models, m)
			}
		}
	}
	return config.APIKey{
		Key:          strings.TrimSpace(item.Key),
		Name:         strings.TrimSpace(item.Name),
		Remark:       strings.TrimSpace(item.Remark),
		ToolsEnabled: item.ToolsEnabled,
		Accounts:     accounts,
		Models:       models,
		QuotaTokens:  item.QuotaTokens,
	}
}

func apiKeyHasMetadata(item config.APIKey) bool {
	return strings.TrimSpace(item.Name) != "" || strings.TrimSpace(item.Remark) != ""
}

func mergeAPIKeysPreferStructured(existing, incoming []config.APIKey) ([]config.APIKey, int) {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil, 0
	}

	merged := make([]config.APIKey, 0, len(existing)+len(incoming))
	index := make(map[string]int, len(existing)+len(incoming))
	for _, item := range existing {
		item = normalizeAPIKeyForStorage(item)
		if item.Key == "" {
			continue
		}
		if _, ok := index[item.Key]; ok {
			continue
		}
		index[item.Key] = len(merged)
		merged = append(merged, item)
	}

	imported := 0
	for _, item := range incoming {
		item = normalizeAPIKeyForStorage(item)
		if item.Key == "" {
			continue
		}
		if idx, ok := index[item.Key]; ok {
			keep := merged[idx]
			next := mergeAPIKeyRecord(keep, item)
			if !equalAPIKey(next, keep) {
				merged[idx] = next
				imported++
			}
			continue
		}
		index[item.Key] = len(merged)
		merged = append(merged, item)
		imported++
	}

	if len(merged) == 0 {
		return nil, imported
	}
	return merged, imported
}

func equalAPIKey(a, b config.APIKey) bool {
	return strings.TrimSpace(a.Key) == strings.TrimSpace(b.Key) &&
		strings.TrimSpace(a.Name) == strings.TrimSpace(b.Name) &&
		strings.TrimSpace(a.Remark) == strings.TrimSpace(b.Remark) &&
		a.ToolsEnabled == b.ToolsEnabled &&
		slices.Equal(a.Accounts, b.Accounts) &&
		slices.Equal(a.Models, b.Models) &&
		a.QuotaTokens == b.QuotaTokens
}

func mergeAPIKeyRecord(existing, incoming config.APIKey) config.APIKey {
	existing = normalizeAPIKeyForStorage(existing)
	incoming = normalizeAPIKeyForStorage(incoming)
	if existing.Key == "" {
		return incoming
	}
	var keep config.APIKey
	if apiKeyHasMetadata(existing) {
		keep = existing
	} else if apiKeyHasMetadata(incoming) {
		keep = incoming
	} else {
		keep = existing
	}
	keep.ToolsEnabled = existing.ToolsEnabled || incoming.ToolsEnabled

	// Policy merge precedence: local (existing) policy takes precedence over imported (incoming) policy
	if len(existing.Accounts) > 0 {
		keep.Accounts = existing.Accounts
	} else {
		keep.Accounts = incoming.Accounts
	}
	if len(existing.Models) > 0 {
		keep.Models = existing.Models
	} else {
		keep.Models = incoming.Models
	}
	if existing.QuotaTokens > 0 {
		keep.QuotaTokens = existing.QuotaTokens
	} else {
		keep.QuotaTokens = incoming.QuotaTokens
	}

	return keep
}

func fieldString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func fieldStringOptional(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v)), true
}

func fieldBoolOptional(m map[string]any, key string) (bool, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return false, false
	}
	return util.ToBool(v), true
}

func fieldStringSlice(m map[string]any, key string) ([]string, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	if v == nil {
		return []string{}, true
	}
	switch raw := v.(type) {
	case []string:
		out := make([]string, 0, len(raw))
		for _, s := range raw {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, true
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s := strings.TrimSpace(fmt.Sprintf("%v", item)); s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out, true
	case string:
		s := strings.TrimSpace(raw)
		if s == "" {
			return []string{}, true
		}
		return []string{s}, true
	default:
		return nil, false
	}
}

func fieldInt64Optional(m map[string]any, key string) (int64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	if v == nil {
		return 0, true
	}
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case json.Number:
		val, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return val, true
	case string:
		n = strings.TrimSpace(n)
		if n == "" {
			return 0, true
		}
		val, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return val, true
	default:
		return 0, false
	}
}

func statusOr(v int, d int) int {
	if v == 0 {
		return d
	}
	return v
}

func accountMatchesIdentifier(acc config.Account, identifier string) bool {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return false
	}
	if strings.TrimSpace(acc.Email) == id {
		return true
	}
	if mobileKey := config.CanonicalMobileKey(id); mobileKey != "" && mobileKey == config.CanonicalMobileKey(acc.Mobile) {
		return true
	}
	return acc.Identifier() == id
}

func normalizeAccountForStorage(acc config.Account) config.Account {
	acc.Name = strings.TrimSpace(acc.Name)
	acc.Remark = strings.TrimSpace(acc.Remark)
	acc.Email = strings.TrimSpace(acc.Email)
	acc.Mobile = config.NormalizeMobileForStorage(acc.Mobile)
	acc.DeviceID = strings.TrimSpace(acc.DeviceID)
	acc.ProxyID = strings.TrimSpace(acc.ProxyID)
	acc.PoolType = config.NormalizePoolType(acc.PoolType)
	acc.Locale = strings.TrimSpace(acc.Locale)
	return acc
}

func findProxyByID(c config.Config, proxyID string) (config.Proxy, bool) {
	id := strings.TrimSpace(proxyID)
	if id == "" {
		return config.Proxy{}, false
	}
	for _, proxy := range c.Proxies {
		proxy = config.NormalizeProxy(proxy)
		if proxy.ID == id {
			return proxy, true
		}
	}
	return config.Proxy{}, false
}

func accountDedupeKey(acc config.Account) string {
	if email := strings.TrimSpace(acc.Email); email != "" {
		return "email:" + email
	}
	if mobile := config.CanonicalMobileKey(acc.Mobile); mobile != "" {
		return "mobile:" + mobile
	}
	if id := strings.TrimSpace(acc.Identifier()); id != "" {
		return "id:" + id
	}
	return ""
}

func normalizeAndDedupeAccounts(accounts []config.Account) []config.Account {
	if len(accounts) == 0 {
		return nil
	}
	out := make([]config.Account, 0, len(accounts))
	seen := make(map[string]struct{}, len(accounts))
	for _, acc := range accounts {
		acc = normalizeAccountForStorage(acc)
		key := accountDedupeKey(acc)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, acc)
	}
	return out
}

func findAccountByIdentifier(store ConfigStore, identifier string) (config.Account, bool) {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return config.Account{}, false
	}
	if acc, ok := store.FindAccount(id); ok {
		return acc, true
	}
	accounts := store.Snapshot().Accounts
	for _, acc := range accounts {
		if accountMatchesIdentifier(acc, id) {
			return acc, true
		}
	}
	return config.Account{}, false
}

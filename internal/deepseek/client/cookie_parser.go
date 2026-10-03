package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ds2api/internal/config"
)

// ParsedDeepSeekSession holds the extracted JWT token and normalized cookies
// from a raw user input (JSON export, Cookie string, or pure JWT).
type ParsedDeepSeekSession struct {
	Token        string            `json:"token"`
	CookieHeader string            `json:"cookie_header"`
	CookiesMap   map[string]string `json:"cookies_map"`
}

var tokenKeyNames = map[string]bool{
	"usertoken":     true,
	"token":         true,
	"authorization": true,
	"auth_token":    true,
	"access_token":  true,
	"bearer":        true,
}

func isTokenKey(k string) bool {
	clean := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(k), "-", "_"))
	return tokenKeyNames[clean]
}

func cleanTokenValue(v string) string {
	return config.CleanWrappedToken(v)
}

func isLikelyJWT(s string) bool {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "bearer ") {
		s = strings.TrimSpace(s[7:])
	}
	s = strings.Trim(s, `"'`)
	if !strings.HasPrefix(s, "ey") {
		return false
	}
	// JWTs have at least two dots (header.payload.signature)
	return strings.Count(s, ".") >= 2 && !strings.ContainsAny(s, " \t\r\n;=")
}

// ParseDeepSeekSession parses a raw session/cookie input string into a DeepSeek JWT token
// and normalized cookie header.
func ParseDeepSeekSession(raw string) (*ParsedDeepSeekSession, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty deepseek session or cookies")
	}

	result := &ParsedDeepSeekSession{
		CookiesMap: make(map[string]string),
	}

	// 1. Check if raw input is a pure JWT / Bearer JWT string
	if isLikelyJWT(raw) {
		result.Token = cleanTokenValue(raw)
		return result, nil
	}

	// 2. Try JSON object: {"token": "...", "cookies": "..."} or flat map
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err == nil && len(obj) > 0 {
		parseJSONObject(obj, result)
		result.CookieHeader = buildCookieHeader(result.CookiesMap)
		if result.Token == "" && len(result.CookiesMap) == 0 {
			return nil, errors.New("no valid token or cookies found in JSON")
		}
		return result, nil
	}

	// 3. Try JSON array of cookie objects: [{"name": "...", "value": "..."}]
	var arr []map[string]any
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		for _, item := range arr {
			name, _ := item["name"].(string)
			val, _ := item["value"].(string)
			name = strings.TrimSpace(name)
			val = strings.TrimSpace(val)
			if name == "" {
				continue
			}
			if isTokenKey(name) && result.Token == "" {
				result.Token = cleanTokenValue(val)
			} else {
				result.CookiesMap[name] = val
			}
		}
		result.CookieHeader = buildCookieHeader(result.CookiesMap)
		if result.Token == "" && len(result.CookiesMap) == 0 {
			return nil, errors.New("no valid token or cookies found in JSON array")
		}
		return result, nil
	}

	// 4. Semicolon or newline-delimited cookie string
	parseDelimitedString(raw, result)
	result.CookieHeader = buildCookieHeader(result.CookiesMap)

	if result.Token == "" && len(result.CookiesMap) == 0 {
		return nil, errors.New("no valid token or cookies found")
	}

	return result, nil
}

func parseJSONObject(obj map[string]any, result *ParsedDeepSeekSession) {
	for k, v := range obj {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}

		if isTokenKey(key) && result.Token == "" {
			switch tv := v.(type) {
			case string:
				result.Token = cleanTokenValue(tv)
				continue
			case map[string]any:
				for _, tk := range []string{"value", "token", "userToken", "usertoken", "access_token", "authToken", "auth_token"} {
					if val, ok := tv[tk].(string); ok && strings.TrimSpace(val) != "" {
						result.Token = cleanTokenValue(val)
						break
					}
				}
				continue
			}
		}

		if strings.EqualFold(key, "cookies") {
			switch cv := v.(type) {
			case string:
				parseDelimitedString(cv, result)
			case map[string]any:
				for ck, cval := range cv {
					if cstr, ok := cval.(string); ok {
						ckClean := strings.TrimSpace(ck)
						if isTokenKey(ckClean) && result.Token == "" {
							result.Token = cleanTokenValue(cstr)
						} else if ckClean != "" {
							result.CookiesMap[ckClean] = strings.TrimSpace(cstr)
						}
					}
				}
			case []any:
				for _, citem := range cv {
					if cmap, ok := citem.(map[string]any); ok {
						name, _ := cmap["name"].(string)
						val, _ := cmap["value"].(string)
						name = strings.TrimSpace(name)
						val = strings.TrimSpace(val)
						if name == "" {
							continue
						}
						if isTokenKey(name) && result.Token == "" {
							result.Token = cleanTokenValue(val)
						} else {
							result.CookiesMap[name] = val
						}
					}
				}
			}
			continue
		}

		if strVal, ok := v.(string); ok {
			strVal = strings.TrimSpace(strVal)
			if isTokenKey(key) && result.Token == "" {
				result.Token = cleanTokenValue(strVal)
			} else {
				result.CookiesMap[key] = strVal
			}
		}
	}
}

func parseDelimitedString(raw string, result *ParsedDeepSeekSession) {
	// Normalize separators: replace newlines with semicolons
	normalized := strings.ReplaceAll(raw, "\n", ";")
	pairs := strings.Split(normalized, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		idx := strings.IndexByte(p, '=')
		if idx < 0 {
			idx = strings.IndexByte(p, ':')
		}
		if idx > 0 {
			k := strings.TrimSpace(p[:idx])
			v := strings.TrimSpace(p[idx+1:])
			if k == "" {
				continue
			}
			if isTokenKey(k) && result.Token == "" {
				result.Token = cleanTokenValue(v)
			} else {
				result.CookiesMap[k] = v
			}
		} else if isLikelyJWT(p) && result.Token == "" {
			result.Token = cleanTokenValue(p)
		}
	}

	// Fallback: If token was saved in cookie map under a token key, extract it
	for k, v := range result.CookiesMap {
		if isTokenKey(k) && result.Token == "" {
			result.Token = cleanTokenValue(v)
			delete(result.CookiesMap, k)
		}
	}
}

func buildCookieHeader(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s=%s", name, m[name]))
	}
	return strings.Join(parts, "; ")
}

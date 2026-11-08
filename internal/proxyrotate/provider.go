// Package proxyrotate resolves rotating proxy sources ("proxy xoay") into the
// concrete HTTP/SOCKS5 endpoints the transports understand.
//
// A config.Proxy that carries a Rotate block has no fixed address: this package
// asks the provider API for a fresh exit node, keeps that node while it is
// still healthy, and renews it shortly before it dies. Everything else in the
// stack keeps working with plain host/port proxies, so the rotation concern
// stays inside this boundary.
package proxyrotate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ds2api/internal/config"
)

// statusOK is the provider success code. 101/102 mean the key is missing or
// invalid, per the vendor documentation.
const statusOK = 100

const (
	maxResponseBytes = 64 << 10
	fetchTimeout     = 12 * time.Second
)

// presetEndpoints maps a provider name to its documented "get proxy" URL. The
// Rotate config can always override it.
var presetEndpoints = map[string]string{
	config.ProxyRotateProviderProxyXoay: "https://proxyxoay.shop/api/get.php",
	config.ProxyRotateProviderBestProxy: "https://bestproxy.vn/proxyxoay/get.php",
}

// EndpointFor returns the fetch URL for a normalized rotate config.
func EndpointFor(rotate config.RotateProxyConfig) (string, error) {
	if endpoint := strings.TrimSpace(rotate.Endpoint); endpoint != "" {
		return endpoint, nil
	}
	if preset, ok := presetEndpoints[rotate.Provider]; ok {
		return preset, nil
	}
	return "", fmt.Errorf("no endpoint configured for rotate provider %q", rotate.Provider)
}

// RequestURL builds the provider GET URL. The vendor documents GET with the
// four parameters below; empty values are still sent so the request matches the
// sample link in their docs.
func RequestURL(rotate config.RotateProxyConfig) (string, error) {
	base, err := EndpointFor(rotate)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid rotate endpoint %q: %w", base, err)
	}
	query := parsed.Query()
	query.Set("key", rotate.Key)
	query.Set("nhamang", rotate.Network)
	query.Set("tinhthanh", rotate.Province)
	query.Set("whitelist", rotate.Whitelist)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// node is one freshly fetched exit node.
type node struct {
	proxy     config.Proxy
	expiresAt time.Time
	network   string
	location  string
	message   string
}

// payloadFieldNames maps our normalized field names onto the vendor keys,
// which are a mix of snake_case and Vietnamese-labelled display fields.
var payloadFieldNames = map[string][]string{
	"status":     {"status"},
	"message":    {"message", "comen"},
	"http":       {"proxyhttp", "proxy_http", "http"},
	"socks5":     {"proxysocks5", "proxy_socks5", "socks5"},
	"network":    {"nha mang", "nhamang", "network", "provider"},
	"location":   {"vi tri", "vitri", "location", "city"},
	"token_date": {"token expiration date", "expired", "token_expiry"},
}

type payload map[string]any

func (p payload) stringField(name string) string {
	for _, key := range payloadFieldNames[name] {
		for actual, value := range p {
			if strings.EqualFold(strings.TrimSpace(actual), key) {
				if text := strings.TrimSpace(fmt.Sprintf("%v", value)); text != "" && text != "<nil>" {
					return text
				}
			}
		}
	}
	return ""
}

func (p payload) status() (int, bool) {
	for _, key := range payloadFieldNames["status"] {
		for actual, value := range p {
			if !strings.EqualFold(strings.TrimSpace(actual), key) {
				continue
			}
			switch raw := value.(type) {
			case float64:
				return int(raw), true
			case json.Number:
				n, err := raw.Int64()
				if err != nil {
					return 0, false
				}
				return int(n), true
			case string:
				n, err := strconv.Atoi(strings.TrimSpace(raw))
				if err != nil {
					return 0, false
				}
				return n, true
			}
		}
	}
	return 0, false
}

var (
	tTLExplicit = regexp.MustCompile(`(?i)(?:die sau|expires? in|expir\w*\s+in|c[oò]n|remaining)\D{0,8}(\d{1,6})\s*s(?:econds?)?\b`)
	tTLLoose    = regexp.MustCompile(`(?i)(\d{1,6})\s*(?:s\b|seconds?\b)`)
)

// tokenDateLayouts covers both the 4-digit and 2-digit year shapes seen from
// the vendors ("22:52 19-02-2025" and "21:43 29-03-25").
var tokenDateLayouts = []string{"15:04 02-01-2006", "15:04 02-01-06", "15:04 02/01/2006", "2006-01-02 15:04:05", time.RFC3339}

func parseTokenExpiration(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range tokenDateLayouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// ttlFromMessage extracts the lifetime the vendor prints next to the proxy,
// for example "proxy nay se die sau 1777s".
func ttlFromMessage(message string) (int, bool) {
	if match := tTLExplicit.FindStringSubmatch(message); len(match) == 2 {
		if seconds, err := strconv.Atoi(match[1]); err == nil {
			return seconds, true
		}
	}
	if match := tTLLoose.FindStringSubmatch(message); len(match) == 2 {
		if seconds, err := strconv.Atoi(match[1]); err == nil {
			return seconds, true
		}
	}
	return 0, false
}

// parseEndpoint converts the vendor "ip:port::" (or "ip:port:user:pass") shape
// into host, port and optional credentials.
func parseEndpoint(raw string) (host string, port int, username string, password string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0, "", "", fmt.Errorf("empty proxy endpoint")
	}
	if strings.Count(raw, ":") > 3 {
		return "", 0, "", "", fmt.Errorf("unsupported proxy endpoint %q", raw)
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 {
		return "", 0, "", "", fmt.Errorf("proxy endpoint %q has no port", raw)
	}
	host = strings.TrimSpace(parts[0])
	port, err = strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || port < 1 || port > 65535 {
		return "", 0, "", "", fmt.Errorf("proxy endpoint %q has an invalid port", raw)
	}
	if host == "" {
		return "", 0, "", "", fmt.Errorf("proxy endpoint %q has no host", raw)
	}
	if ip := net.ParseIP(host); ip == nil {
		return "", 0, "", "", fmt.Errorf("proxy endpoint %q has a non-IPv4 host", raw)
	}
	if len(parts) >= 4 {
		username = strings.TrimSpace(parts[2])
		password = strings.TrimSpace(parts[3])
	}
	return host, port, username, password, nil
}

// ParseProviderResponse turns a provider JSON body into a usable node.
// fallbackTTL is applied when the response carries no lifetime information.
func ParseProviderResponse(body []byte, rotate config.RotateProxyConfig, now time.Time) (node, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded payload
	if err := decoder.Decode(&decoded); err != nil {
		preview := string(body)
		if len(preview) > 180 {
			preview = preview[:180] + "..."
		}
		return node{}, fmt.Errorf("provider returned a non-JSON body: %s", preview)
	}

	status, ok := decoded.status()
	if !ok {
		return node{}, fmt.Errorf("provider response has no status field")
	}
	message := decoded.stringField("message")
	if status != statusOK {
		if message == "" {
			message = "unknown provider error"
		}
		return node{}, fmt.Errorf("provider status=%d: %s", status, message)
	}

	raw := decoded.stringField("http")
	protocol := "http"
	if rotate.Protocol == "socks5" {
		raw = decoded.stringField("socks5")
		protocol = "socks5"
		if raw == "" {
			// Some plans only publish the HTTP field; keep working instead of
			// failing the whole request.
			raw = decoded.stringField("http")
			protocol = "http"
		}
	}
	if raw == "" {
		return node{}, fmt.Errorf("provider response has no usable proxy field")
	}

	host, port, username, password, err := parseEndpoint(raw)
	if err != nil {
		return node{}, err
	}

	ttl := rotate.TTLSeconds
	if reported, ok := ttlFromMessage(message); ok {
		if clamped, valid := config.RotateTTLBounds(reported); valid {
			ttl = clamped
		}
	}
	expiresAt := now.Add(time.Duration(ttl) * time.Second)
	if keyExpiry, ok := parseTokenExpiration(decoded.stringField("token_date")); ok && keyExpiry.Before(expiresAt) && keyExpiry.After(now) {
		expiresAt = keyExpiry
	}

	return node{
		proxy: config.Proxy{
			Type:     protocol,
			Host:     host,
			Port:     port,
			Username: username,
			Password: password,
		},
		expiresAt: expiresAt,
		network:   decoded.stringField("network"),
		location:  decoded.stringField("location"),
		message:   message,
	}, nil
}

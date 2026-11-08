package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func ValidateConfig(c Config) error {
	if err := ValidateProxyConfig(c.Proxies); err != nil {
		return err
	}
	if err := ValidateAdminConfig(c.Admin); err != nil {
		return err
	}
	if err := ValidateRuntimeConfig(c.Runtime); err != nil {
		return err
	}
	if err := ValidateResponsesConfig(c.Responses); err != nil {
		return err
	}
	if err := ValidateEmbeddingsConfig(c.Embeddings); err != nil {
		return err
	}
	if err := ValidateAutoDeleteConfig(c.AutoDelete); err != nil {
		return err
	}
	if err := ValidateCurrentInputFileConfig(c.CurrentInputFile); err != nil {
		return err
	}
	if err := ValidateExpertPromptSegmentConfig(c.ExpertPromptSegment); err != nil {
		return err
	}
	if err := ValidateExpertTextFileInlineConfig(c.ExpertTextFileInline); err != nil {
		return err
	}
	if err := ValidateAutoRouteVisionConfig(c.AutoRouteVision); err != nil {
		return err
	}
	if err := ValidateAccountProxyReferences(c.Accounts, c.Proxies); err != nil {
		return err
	}
	return nil
}

func ValidateProxyConfig(proxies []Proxy) error {
	seen := make(map[string]struct{}, len(proxies))
	for _, proxy := range proxies {
		proxy = NormalizeProxy(proxy)
		if err := ValidateTrimmedString("proxies.id", proxy.ID, true); err != nil {
			return err
		}
		if proxy.Rotate != nil {
			if err := ValidateRotateProxyConfig(proxy); err != nil {
				return err
			}
		} else {
			switch proxy.Type {
			case "socks5", "socks5h", "http", "https":
			default:
				return fmt.Errorf("proxies.type must be one of socks5, socks5h, http, https")
			}
			if err := ValidateTrimmedString("proxies.host", proxy.Host, true); err != nil {
				return err
			}
			if err := ValidateIntRange("proxies.port", proxy.Port, 1, 65535, true); err != nil {
				return err
			}
		}
		if _, ok := seen[proxy.ID]; ok {
			return fmt.Errorf("duplicate proxy id: %s", proxy.ID)
		}
		seen[proxy.ID] = struct{}{}
	}
	return nil
}

// ValidateRotateProxyConfig checks a rotating proxy entry. Host/Port are not
// part of the contract: the endpoint comes from the provider API at runtime.
func ValidateRotateProxyConfig(proxy Proxy) error {
	rotate := NormalizeRotateProxy(proxy.Rotate)
	if rotate == nil {
		return fmt.Errorf("proxies.rotate cannot be empty")
	}
	if err := ValidateTrimmedString("proxies.rotate.key", rotate.Key, true); err != nil {
		return err
	}
	switch rotate.Provider {
	case ProxyRotateProviderProxyXoay, ProxyRotateProviderBestProxy, ProxyRotateProviderCustom:
	default:
		return fmt.Errorf("proxies.rotate.provider must be one of %s, %s, %s",
			ProxyRotateProviderProxyXoay, ProxyRotateProviderBestProxy, ProxyRotateProviderCustom)
	}
	if rotate.Provider == ProxyRotateProviderCustom && strings.TrimSpace(rotate.Endpoint) == "" {
		return fmt.Errorf("proxies.rotate.endpoint is required when provider is custom")
	}
	if rotate.Endpoint != "" {
		parsed, err := url.Parse(rotate.Endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("proxies.rotate.endpoint must be an absolute http(s) URL")
		}
	}
	switch rotate.Protocol {
	case "http", "socks5":
	default:
		return fmt.Errorf("proxies.rotate.protocol must be one of http, socks5")
	}
	if err := ValidateIntRange("proxies.rotate.ttl_seconds", rotate.TTLSeconds, 1, DefaultRotateMaxTTLSeconds, false); err != nil {
		return err
	}
	if err := ValidateIntRange("proxies.rotate.min_remaining_seconds", rotate.MinRemainingSeconds, 1, DefaultRotateMaxTTLSeconds, false); err != nil {
		return err
	}
	return ValidateWhitelistIPv4(rotate.Whitelist)
}

// ValidateWhitelistIPv4 accepts a comma separated list of IPv4 addresses, which
// is what the rotating proxy providers expect in `whitelist`.
func ValidateWhitelistIPv4(list string) error {
	list = strings.TrimSpace(list)
	if list == "" {
		return nil
	}
	for _, item := range strings.Split(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return fmt.Errorf("proxies.rotate.whitelist contains an empty entry")
		}
		parsed := net.ParseIP(item)
		if parsed == nil || parsed.To4() == nil {
			return fmt.Errorf("proxies.rotate.whitelist must be IPv4 address(es): %s", item)
		}
	}
	return nil
}

func ValidateAccountProxyReferences(accounts []Account, proxies []Proxy) error {
	if len(accounts) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(proxies))
	for _, proxy := range proxies {
		ids[NormalizeProxy(proxy).ID] = struct{}{}
	}
	for _, acc := range accounts {
		proxyID := strings.TrimSpace(acc.ProxyID)
		if proxyID == "" {
			continue
		}
		if _, ok := ids[proxyID]; !ok {
			return fmt.Errorf("account proxy_id references unknown proxy: %s", proxyID)
		}
	}
	return nil
}

func ValidateAdminConfig(admin AdminConfig) error {
	return ValidateIntRange("admin.jwt_expire_hours", admin.JWTExpireHours, 1, 720, false)
}

func ValidateRuntimeConfig(runtime RuntimeConfig) error {
	if err := ValidateIntRange("runtime.account_max_inflight", runtime.AccountMaxInflight, 1, 256, false); err != nil {
		return err
	}
	if err := ValidateIntRange("runtime.account_max_queue", runtime.AccountMaxQueue, 1, 200000, false); err != nil {
		return err
	}
	if err := ValidateIntRange("runtime.global_max_inflight", runtime.GlobalMaxInflight, 1, 200000, false); err != nil {
		return err
	}
	if err := ValidateIntRange("runtime.token_refresh_interval_hours", runtime.TokenRefreshIntervalHours, 1, 720, false); err != nil {
		return err
	}
	if runtime.AccountMaxInflight > 0 && runtime.GlobalMaxInflight > 0 && runtime.GlobalMaxInflight < runtime.AccountMaxInflight {
		return fmt.Errorf("runtime.global_max_inflight must be >= runtime.account_max_inflight")
	}
	return nil
}

func ValidateResponsesConfig(responses ResponsesConfig) error {
	return ValidateIntRange("responses.store_ttl_seconds", responses.StoreTTLSeconds, 30, 86400, false)
}

func ValidateEmbeddingsConfig(embeddings EmbeddingsConfig) error {
	return ValidateTrimmedString("embeddings.provider", embeddings.Provider, false)
}

func ValidateAutoDeleteConfig(autoDelete AutoDeleteConfig) error {
	return ValidateAutoDeleteMode(autoDelete.Mode)
}

func ValidateCurrentInputFileConfig(currentInputFile CurrentInputFileConfig) error {
	if currentInputFile.MinChars != 0 {
		return ValidateIntRange("current_input_file.min_chars", currentInputFile.MinChars, 1, 100000000, true)
	}
	return nil
}

func ValidateExpertPromptSegmentConfig(cfg ExpertPromptSegmentConfig) error {
	if cfg.MaxChars != 0 {
		if err := ValidateIntRange("expert_prompt_segment.max_chars", cfg.MaxChars, 1000, 100000000, true); err != nil {
			return err
		}
	}
	return nil
}

func ValidateExpertTextFileInlineConfig(cfg ExpertTextFileInlineConfig) error {
	if cfg.MaxFileBytes != 0 {
		if err := ValidateIntRange("expert_text_file_inline.max_file_bytes", cfg.MaxFileBytes, 1, 100<<20, true); err != nil {
			return err
		}
	}
	for i, ext := range cfg.AllowedExtensions {
		trimmed := strings.TrimSpace(ext)
		if trimmed == "" {
			return fmt.Errorf("expert_text_file_inline.allowed_extensions[%d] cannot be empty", i)
		}
	}
	return nil
}

func ValidateAutoRouteVisionConfig(_ AutoRouteVisionConfig) error {
	return nil
}

func ValidateIntRange(name string, value, min, max int, required bool) error {
	if value == 0 && !required {
		return nil
	}
	if value < min || value > max {
		return fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return nil
}

func ValidateTrimmedString(name, value string, required bool) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if !required && value == "" {
			return nil
		}
		return fmt.Errorf("%s cannot be empty", name)
	}
	return nil
}

func ValidateAutoDeleteMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "none", "single", "all":
		return nil
	default:
		return fmt.Errorf("auto_delete.mode must be one of none, single, all")
	}
}

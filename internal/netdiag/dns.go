package netdiag

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var (
	dnsHeaderRegex = regexp.MustCompile(`(?i)(dns[_\s-]*servers?|máy chủ dns|dns-server).*?:\s*([^\r\n]+)`)
)

func parseResolvConf(content string) []string {
	var servers []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "nameserver") {
			ip := fields[1]
			if parsed := net.ParseIP(ip); parsed != nil {
				clean := parsed.String()
				if !seen[clean] {
					seen[clean] = true
					servers = append(servers, clean)
				}
			}
		}
	}
	return servers
}

func parseWindowsIPConfig(output string) []string {
	var servers []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(output))
	inDNSBlock := false

	addIP := func(raw string) {
		clean := strings.TrimSpace(raw)
		if idx := strings.Index(clean, "("); idx != -1 {
			clean = strings.TrimSpace(clean[:idx])
		}
		if parsed := net.ParseIP(clean); parsed != nil {
			ipStr := parsed.String()
			if !seen[ipStr] {
				seen[ipStr] = true
				servers = append(servers, ipStr)
			}
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inDNSBlock = false
			continue
		}

		if match := dnsHeaderRegex.FindStringSubmatch(line); len(match) > 2 {
			inDNSBlock = true
			addIP(match[2])
			continue
		}

		if inDNSBlock {
			if net.ParseIP(trimmed) != nil {
				addIP(trimmed)
				continue
			}
			if strings.Contains(line, ":") {
				inDNSBlock = false
				continue
			}
			inDNSBlock = false
		}
	}
	return servers
}

func parseScutilDNS(output string) []string {
	var servers []string
	seen := make(map[string]bool)

	re := regexp.MustCompile(`(?i)nameserver\[\d+\]\s*:\s*([^\s]+)`)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if match := re.FindStringSubmatch(line); len(match) > 1 {
			ip := match[1]
			if parsed := net.ParseIP(ip); parsed != nil {
				clean := parsed.String()
				if !seen[clean] {
					seen[clean] = true
					servers = append(servers, clean)
				}
			}
		}
	}
	return servers
}

func DetectDNSServers(ctx context.Context) []string {
	ctxTimeout, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var servers []string

	switch runtime.GOOS {
	case "windows":
		cmd := exec.CommandContext(ctxTimeout, "ipconfig", "/all")
		out, err := cmd.Output()
		if err == nil {
			servers = parseWindowsIPConfig(string(out))
		}
	case "linux":
		data, err := os.ReadFile("/etc/resolv.conf")
		if err == nil {
			servers = parseResolvConf(string(data))
		}
	case "darwin":
		cmd := exec.CommandContext(ctxTimeout, "scutil", "--dns")
		out, err := cmd.Output()
		if err == nil {
			servers = parseScutilDNS(string(out))
		}
	}

	if len(servers) == 0 {
		return []string{}
	}
	return servers
}

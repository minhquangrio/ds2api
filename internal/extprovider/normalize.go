package extprovider

import (
	"net/url"
	"strings"
)

func NormalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""

	path := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(path, "/models") {
		path = strings.TrimSuffix(path, "/models")
	} else if strings.HasSuffix(path, "/responses") {
		path = strings.TrimSuffix(path, "/responses")
	}

	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/v1"
	}
	u.Path = path

	res := u.String()
	return strings.TrimRight(res, "/")
}

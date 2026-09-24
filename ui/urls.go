package ui

import (
	"net/url"
	"strings"
)

// normalizeURL trims spaces and trailing slashes and adds https:// when no protocol is given.
func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return u
	}
	u = strings.TrimRight(u, "/")
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}
	return u
}

// appendAuthPathToURL appends the authPath query param to baseURL when authPath is non-empty.
func appendAuthPathToURL(baseURL string, authPath string) string {
	authPath = strings.TrimSpace(authPath)
	if authPath == "" {
		return baseURL
	}
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	return baseURL + sep + "authPath=" + url.QueryEscape(authPath)
}

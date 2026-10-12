// Package proxy picks the outbound proxy for agent processes.
// The admin value and OUTBOUND_PROXY in the env file are both live sources:
// whichever was set later wins. An empty result falls back to HTTPS_PROXY,
// then HTTP_PROXY.
package proxy

import (
	"bufio"
	"os"
	"strings"
	"time"
)

// Pick chooses the proxy URL. envAt is the modification time of the env file.
// A later env change overrides the admin value, and a later admin save overrides the file.
func Pick(adminURL string, adminAt time.Time, envURL string, envAt time.Time, fallback string) string {
	adminURL = strings.TrimSpace(adminURL)
	envURL = strings.TrimSpace(envURL)
	fallback = strings.TrimSpace(fallback)
	switch {
	case envURL != "" && (adminURL == "" || envAt.After(adminAt)):
		return envURL
	case adminURL != "":
		return adminURL
	case envURL != "":
		return envURL
	default:
		return fallback
	}
}

// FromProcess returns the proxy inherited by the process itself.
func FromProcess() string {
	if v := strings.TrimSpace(os.Getenv("HTTPS_PROXY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("https_proxy")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("HTTP_PROXY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("http_proxy"))
}

// FileValue reads key from a dotenv-style file. The boolean is false when the
// file is missing. The time is the file modification time.
func FileValue(path, key string) (string, time.Time, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", time.Time{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", info.ModTime(), false
	}
	defer f.Close()
	val := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		val = trimQuotes(strings.TrimSpace(v))
	}
	return val, info.ModTime(), true
}

func trimQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// Resolve is the value agents should use right now.
func Resolve(adminURL string, adminAt time.Time, envFile string) string {
	envURL, envAt, ok := FileValue(envFile, "OUTBOUND_PROXY")
	if !ok {
		envURL = strings.TrimSpace(os.Getenv("OUTBOUND_PROXY"))
		envAt = time.Time{}
	}
	return Pick(adminURL, adminAt, envURL, envAt, FromProcess())
}

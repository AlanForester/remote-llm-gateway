// Package procenv builds environments for child processes.
// A model-controlled shell gets a short allowlist. Operator processes keep
// the parent environment except the gateway's own credentials.
package procenv

import (
	"os"
	"strings"
)

// Sensitive reports names that carry gateway credentials.
func Sensitive(key string) bool {
	if key == "REMOTE_LLM_CLEAN" || key == "REMOTE_LLM_SECRET_FILE" {
		return false
	}
	upper := strings.ToUpper(key)
	if upper == "REDIS_URL" {
		return true
	}
	for _, part := range []string{"TOKEN", "SECRET", "PASSWORD", "API_KEY", "PASSWD"} {
		if strings.Contains(upper, part) {
			return true
		}
	}
	return false
}

// ForTool is the environment of a command the model asked to run.
func ForTool() []string {
	allow := []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "USER", "LOGNAME", "TZ"}
	var out []string
	for _, key := range allow {
		if value, ok := os.LookupEnv(key); ok && !strings.ContainsAny(value, "\r\n\x00") {
			out = append(out, key+"="+value)
		}
	}
	if !hasKey(out, "PATH") {
		out = append(out, "PATH=/usr/bin:/bin")
	}
	return out
}

// WithoutGatewaySecrets copies base without gateway credentials.
func WithoutGatewaySecrets(base []string) []string {
	out := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if Sensitive(key) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func hasKey(env []string, key string) bool {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}

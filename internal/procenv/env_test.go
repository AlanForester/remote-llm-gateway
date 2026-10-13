package procenv

import (
	"strings"
	"testing"
)

func TestForToolOmitsSecrets(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("REDIS_URL", "redis://:hidden@redis:6379/0")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAHIDDEN")
	t.Setenv("OPENAI_API_KEY", "sk-hidden")
	got := strings.Join(ForTool(), "\n")
	if strings.Contains(got, "hidden") || strings.Contains(got, "AKIA") || strings.Contains(got, "REDIS") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "PATH=/usr/bin") {
		t.Fatalf("%s", got)
	}
}

func TestWithoutGatewaySecrets(t *testing.T) {
	base := []string{"PATH=/bin", "ADMIN_TOKEN=secret", "HOME=/tmp", "REDIS_URL=redis://localhost"}
	got := strings.Join(WithoutGatewaySecrets(base), "\n")
	if strings.Contains(got, "secret") || strings.Contains(got, "REDIS") {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got, "PATH=/bin") || !strings.Contains(got, "HOME=/tmp") {
		t.Fatalf("%s", got)
	}
}

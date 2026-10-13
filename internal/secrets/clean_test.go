package secrets

import (
	"os"
	"strings"
	"testing"
)

func TestStashRestoresAndDeletesFile(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Unsetenv("ADMIN_TOKEN")
		_ = os.Unsetenv("REDIS_URL")
	})
	env := []string{"PATH=/bin", "ADMIN_TOKEN=secret-value", "REDIS_URL=redis://localhost/0", "HOME=/tmp"}
	path, err := stash(env)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected a file")
	}
	t.Setenv(fileKey, path)
	t.Setenv("ADMIN_TOKEN", "")
	_ = os.Unsetenv("ADMIN_TOKEN")
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("ADMIN_TOKEN") != "secret-value" {
		t.Fatalf("token %q", os.Getenv("ADMIN_TOKEN"))
	}
	if os.Getenv("REDIS_URL") != "redis://localhost/0" {
		t.Fatalf("redis %q", os.Getenv("REDIS_URL"))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("secret file still present: %v", err)
	}
	if os.Getenv(fileKey) != "" {
		t.Fatal("secret path stayed in the environment")
	}
	pub := strings.Join(publicEnv(env), "\n")
	if strings.Contains(pub, "secret-value") || strings.Contains(pub, "REDIS_URL") {
		t.Fatalf("%s", pub)
	}
	if !strings.Contains(pub, "PATH=/bin") || !strings.Contains(pub, "HOME=/tmp") {
		t.Fatalf("%s", pub)
	}
}

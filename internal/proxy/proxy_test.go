package proxy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPickLastWriteWins(t *testing.T) {
	adminAt := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	envBefore := adminAt.Add(-time.Minute)
	envAfter := adminAt.Add(time.Minute)

	if got := Pick("http://admin:1", adminAt, "http://env:1", envBefore, "http://fb"); got != "http://admin:1" {
		t.Fatalf("admin should win when it is newer, got %s", got)
	}
	if got := Pick("http://admin:1", adminAt, "http://env:1", envAfter, "http://fb"); got != "http://env:1" {
		t.Fatalf("env file should win when it is newer, got %s", got)
	}
	if got := Pick("", time.Time{}, "", time.Time{}, "http://fb"); got != "http://fb" {
		t.Fatalf("fallback, got %s", got)
	}
	if got := Pick("  ", adminAt, "", time.Time{}, "http://fb"); got != "http://fb" {
		t.Fatalf("blank admin falls through, got %s", got)
	}
}

func TestValidProxy(t *testing.T) {
	if !Valid("") || !Valid("http://proxy:3128") || !Valid("socks5://127.0.0.1:1080") {
		t.Fatal("expected usable proxy")
	}
	for _, raw := range []string{"http://ok\nEVIL=1", "file:///etc/passwd", "javascript:alert(1)", "/bin/sh"} {
		if Valid(raw) {
			t.Fatalf("accepted %q", raw)
		}
	}
	if got := Pick("http://ok\nEVIL=1", time.Time{}, "", time.Time{}, "http://fallback:3128"); got != "http://fallback:3128" {
		t.Fatalf("invalid admin proxy used: %s", got)
	}
}

func TestFileValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "# comment\nexport OUTBOUND_PROXY=\"http://proxy.example:3128\"\nOTHER=1\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, mod, ok := FileValue(path, "OUTBOUND_PROXY")
	if !ok || got != "http://proxy.example:3128" || mod.IsZero() {
		t.Fatalf("got %q ok=%v mod=%v", got, ok, mod)
	}
	if _, _, ok := FileValue(filepath.Join(dir, "missing"), "OUTBOUND_PROXY"); ok {
		t.Fatal("missing file should not be ok")
	}
}

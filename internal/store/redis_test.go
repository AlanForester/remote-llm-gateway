package store

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestRedisSharesUsersAndQuota(t *testing.T) {
	mr := miniredis.RunT(t)
	url := "redis://" + mr.Addr() + "/0"
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

	first, err := OpenRedis(url)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenRedis(url)
	if err != nil {
		t.Fatal(err)
	}
	user, token, err := first.CreateUser("ann", "claude-sonnet", "day", 5, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := second.Authenticate(token, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != user.ID || got.ForcedModel != "claude-sonnet" {
		t.Fatalf("shared user = %+v", got)
	}
	if _, err := second.AddUsage(user.ID, 3, "claude-sonnet", now); err != nil {
		t.Fatal(err)
	}
	if _, err := first.AddUsage(user.ID, 3, "claude-sonnet", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := second.CheckQuota(user.ID, now.Add(2*time.Minute)); err != ErrQuota {
		t.Fatalf("quota = %v", err)
	}
	later, err := first.CheckQuota(user.ID, now.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if later.TokensUsed != 0 {
		t.Fatalf("counter = %d", later.TokensUsed)
	}
}

func TestOpenConfiguredPicksFileWhenRedisEmpty(t *testing.T) {
	s, err := OpenConfigured(t.TempDir()+"/gateway.json", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateUser("ann", "", "day", 0, time.Now()); err != nil {
		t.Fatal(err)
	}
}

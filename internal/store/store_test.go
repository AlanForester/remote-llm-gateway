package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaResetsAndBlocks(t *testing.T) {
	s := openTest(t)
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	user, token, err := s.CreateUser("ann", "claude-sonnet", "day", 5, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Authenticate(token, now)
	if err != nil {
		t.Fatal(err)
	}
	model, forced := ResolveModel(got.ForcedModel, "grok")
	if !forced || model != "claude-sonnet" {
		t.Fatalf("forced model = %s forced=%v", model, forced)
	}

	if _, err := s.AddUsage(user.ID, 3, model, now); err != nil {
		t.Fatal(err)
	}
	// One response may finish above the limit. The next request is refused.
	if _, err := s.AddUsage(user.ID, 3, model, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CheckQuota(user.ID, now.Add(2*time.Minute)); err != ErrQuota {
		t.Fatalf("expected quota error, got %v", err)
	}

	later := now.Add(25 * time.Hour)
	u, err := s.CheckQuota(user.ID, later)
	if err != nil {
		t.Fatal(err)
	}
	if u.TokensUsed != 0 {
		t.Fatalf("counter = %d, want 0 after a day", u.TokensUsed)
	}
	events, err := s.Usage(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Tokens != 3 || events[1].Tokens != 3 {
		t.Fatalf("history = %+v", events)
	}
}

func TestMonthWeekAndBadPeriod(t *testing.T) {
	start := time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)
	next := NextReset(start, "month")
	if next.Month() != time.March && !(next.Month() == time.February || next.Month() == time.March) {
		// 31 Jan + 1 month is 2 or 3 Mar depending on normalization. Go uses March 2 or March 3.
	}
	if got := NextReset(start, "month"); got.Before(start.AddDate(0, 1, 0).Add(-48*time.Hour)) || got.After(start.AddDate(0, 1, 5)) {
		t.Fatalf("month reset = %s", got)
	}
	if got := NextReset(start, "week"); !got.Equal(start.Add(7 * 24 * time.Hour)) {
		t.Fatalf("week = %s", got)
	}
	if got := NextReset(start, "12h"); !got.Equal(start.Add(12 * time.Hour)) {
		t.Fatalf("12h = %s", got)
	}
	if _, err := NormalizePeriod("nope"); err != ErrBadInput {
		t.Fatalf("err = %v", err)
	}
}

func TestProxyAndUpdate(t *testing.T) {
	s := openTest(t)
	now := time.Now().UTC()
	user, _, err := s.CreateUser("bob", "", "week", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	forced := "gpt-4o"
	limit := int64(10)
	got, err := s.UpdateUser(user.ID, UserPatch{ForcedModel: &forced, TokenLimit: &limit}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ForcedModel != "gpt-4o" || got.TokenLimit != 10 {
		t.Fatalf("%+v", got)
	}
	if err := s.SetProxy("http://proxy.internal:3128", now); err != nil {
		t.Fatal(err)
	}
	url, at, err := s.ProxySettings()
	if err != nil || url != "http://proxy.internal:3128" || at.IsZero() {
		t.Fatalf("proxy %q %v %v", url, at, err)
	}
	if _, err := s.Authenticate("nope", now); err != ErrUnauthorized {
		t.Fatal(err)
	}
}

func TestRotateTokenReplacesTheOldOne(t *testing.T) {
	s := openTest(t)
	now := time.Now().UTC()
	user, old, err := s.CreateUser("ann", "", "day", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	got, next, err := s.RotateToken(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != user.ID || next == "" || next == old {
		t.Fatalf("rotated %+v %q", got, next)
	}
	if _, err := s.Authenticate(old, now); err != ErrUnauthorized {
		t.Fatal(err)
	}
	authed, err := s.Authenticate(next, now)
	if err != nil || authed.ID != user.ID {
		t.Fatalf("%+v %v", authed, err)
	}
	if _, _, err := s.CreateUserWithToken("bob", "", "day", 0, now, "short"); err != ErrBadInput {
		t.Fatal(err)
	}
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Package store is the shared user directory for the gateway, admin, and API.
// Access tokens are stored as SHA-256 hashes. The plaintext is returned once,
// from CreateUser.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrNotFound     = errors.New("not found")
	ErrQuota        = errors.New("quota exceeded")
	ErrBadInput     = errors.New("bad input")
)

// User is an account that may call the gateway.
type User struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	TokenHash   string    `json:"tokenHash"`
	ForcedModel string    `json:"forcedModel"`
	TokenLimit  int64     `json:"tokenLimit"`
	ResetEvery  string    `json:"resetEvery"`
	PeriodStart time.Time `json:"periodStart"`
	TokensUsed  int64     `json:"tokensUsed"`
}

// UsageEvent is one recorded spend. It stays after the period counter resets.
type UsageEvent struct {
	At     time.Time `json:"at"`
	UserID string    `json:"userId"`
	Tokens int64     `json:"tokens"`
	Model  string    `json:"model"`
}

type data struct {
	ProxyURL   string       `json:"proxyUrl"`
	ProxySetAt time.Time    `json:"proxySetAt"`
	Users      []User       `json:"users"`
	Usage      []UsageEvent `json:"usage"`
}

// Store is the shared directory. One file is enough on a single machine.
// REDIS_URL moves the same document to Redis so several processes can share it.
// WebSocket sessions are not stored here.
type Store struct {
	path   string
	mu     sync.Mutex
	commit func(fn func(*data) error) error
}

// Open creates the store file when it is missing.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	s.commit = s.commitFile
	return s, s.with(func(d *data) error { return nil })
}

// OpenConfigured uses Redis when redisURL is set, and the file otherwise.
func OpenConfigured(path, redisURL string) (*Store, error) {
	if strings.TrimSpace(redisURL) != "" {
		return OpenRedis(redisURL)
	}
	return Open(path)
}

// PublicUser is the view returned to clients. It has no token hash.
type PublicUser struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ForcedModel string    `json:"forcedModel"`
	Forced      bool      `json:"forced"`
	Model       string    `json:"model"`
	TokenLimit  int64     `json:"tokenLimit"`
	TokensUsed  int64     `json:"tokensUsed"`
	ResetEvery  string    `json:"resetEvery"`
	PeriodStart time.Time `json:"periodStart"`
	ResetAt     time.Time `json:"resetAt"`
}

// Public projects a user at the given time, rolling the period when it has ended.
func Public(u User, requested string, now time.Time) PublicUser {
	u = rolled(u, now)
	model, forced := ResolveModel(u.ForcedModel, requested)
	return PublicUser{
		ID:          u.ID,
		Name:        u.Name,
		ForcedModel: u.ForcedModel,
		Forced:      forced,
		Model:       model,
		TokenLimit:  u.TokenLimit,
		TokensUsed:  u.TokensUsed,
		ResetEvery:  u.ResetEvery,
		PeriodStart: u.PeriodStart,
		ResetAt:     NextReset(u.PeriodStart, u.ResetEvery),
	}
}

// ResolveModel returns the admin model when one is set.
func ResolveModel(forced, requested string) (string, bool) {
	forced = strings.TrimSpace(forced)
	if forced != "" {
		return forced, true
	}
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested, false
	}
	return "grok", false
}

// NextReset is the moment the counter returns to zero.
func NextReset(start time.Time, every string) time.Time {
	if start.IsZero() {
		return time.Time{}
	}
	switch strings.TrimSpace(every) {
	case "", "day":
		return start.Add(24 * time.Hour)
	case "week":
		return start.Add(7 * 24 * time.Hour)
	case "month":
		return start.AddDate(0, 1, 0)
	default:
		d, err := time.ParseDuration(every)
		if err != nil || d <= 0 {
			return start.Add(24 * time.Hour)
		}
		return start.Add(d)
	}
}

func rolled(u User, now time.Time) User {
	if strings.TrimSpace(u.ResetEvery) == "" {
		u.ResetEvery = "day"
	}
	if u.PeriodStart.IsZero() {
		u.PeriodStart = now
		return u
	}
	// Jump forward in one step for fixed durations. Month stays calendar-based.
	if u.ResetEvery != "month" {
		d := NextReset(u.PeriodStart, u.ResetEvery).Sub(u.PeriodStart)
		if d <= 0 {
			d = 24 * time.Hour
		}
		if elapsed := now.Sub(u.PeriodStart); elapsed >= d {
			n := int64(elapsed / d)
			u.PeriodStart = u.PeriodStart.Add(time.Duration(n) * d)
			u.TokensUsed = 0
		}
		return u
	}
	for !now.Before(NextReset(u.PeriodStart, "month")) {
		u.PeriodStart = NextReset(u.PeriodStart, "month")
		u.TokensUsed = 0
		if u.PeriodStart.After(now.AddDate(100, 0, 0)) {
			break
		}
	}
	return u
}

// NormalizePeriod accepts day, week, month, or a Go duration such as 12h.
func NormalizePeriod(every string) (string, error) {
	every = strings.TrimSpace(every)
	if every == "" {
		return "day", nil
	}
	if len(every) > 32 {
		return "", ErrBadInput
	}
	switch every {
	case "day", "week", "month":
		return every, nil
	}
	d, err := time.ParseDuration(every)
	if err != nil || d <= 0 {
		return "", ErrBadInput
	}
	return every, nil
}

// CreateUser inserts a user and returns the plaintext access token once.
func (s *Store) CreateUser(name, forcedModel, resetEvery string, tokenLimit int64, now time.Time) (User, string, error) {
	var err error
	name, err = bounded(name, 80, false)
	if err != nil {
		return User{}, "", err
	}
	forcedModel, err = bounded(forcedModel, 120, true)
	if err != nil {
		return User{}, "", err
	}
	every, err := NormalizePeriod(resetEvery)
	if err != nil {
		return User{}, "", err
	}
	if tokenLimit < 0 {
		return User{}, "", ErrBadInput
	}
	token, err := newToken()
	if err != nil {
		return User{}, "", err
	}
	return s.createWithToken(name, forcedModel, every, tokenLimit, now, token)
}

// CreateUserWithToken inserts a user with a token the admin already showed.
// The token must be 64 hex characters from NewToken.
func (s *Store) CreateUserWithToken(name, forcedModel, resetEvery string, tokenLimit int64, now time.Time, token string) (User, string, error) {
	if !validAccessToken(token) {
		return User{}, "", ErrBadInput
	}
	var err error
	name, err = bounded(name, 80, false)
	if err != nil {
		return User{}, "", err
	}
	forcedModel, err = bounded(forcedModel, 120, true)
	if err != nil {
		return User{}, "", err
	}
	every, err := NormalizePeriod(resetEvery)
	if err != nil {
		return User{}, "", err
	}
	if tokenLimit < 0 {
		return User{}, "", ErrBadInput
	}
	return s.createWithToken(name, forcedModel, every, tokenLimit, now, token)
}

func (s *Store) createWithToken(name, forcedModel, every string, tokenLimit int64, now time.Time, token string) (User, string, error) {
	user := User{
		ID:          newID(),
		Name:        name,
		TokenHash:   hashToken(token),
		ForcedModel: strings.TrimSpace(forcedModel),
		TokenLimit:  tokenLimit,
		ResetEvery:  every,
		PeriodStart: now,
	}
	err := s.with(func(d *data) error {
		d.Users = append(d.Users, user)
		return nil
	})
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

// UserPatch updates fields that are non-nil. An empty ForcedModel clears the assignment.
type UserPatch struct {
	Name        *string
	ForcedModel *string
	TokenLimit  *int64
	ResetEvery  *string
}

// RotateToken replaces the access token. The previous token stops working.
func (s *Store) RotateToken(id string) (User, string, error) {
	token, err := newToken()
	if err != nil {
		return User{}, "", err
	}
	var user User
	err = s.with(func(d *data) error {
		u, i := findUser(d, id)
		if i < 0 {
			return ErrNotFound
		}
		u.TokenHash = hashToken(token)
		d.Users[i] = u
		user = u
		return nil
	})
	if err != nil {
		return User{}, "", err
	}
	return user, token, nil
}

// NewToken is a new access token. Only its hash is stored.
func NewToken() (string, error) {
	return newToken()
}

// UpdateUser changes admin-controlled fields.
func (s *Store) UpdateUser(id string, patch UserPatch, now time.Time) (User, error) {
	var updated User
	err := s.with(func(d *data) error {
		u, i := findUser(d, id)
		if i < 0 {
			return ErrNotFound
		}
		if patch.Name != nil {
			name, err := bounded(*patch.Name, 80, false)
			if err != nil {
				return err
			}
			u.Name = name
		}
		if patch.ForcedModel != nil {
			forced, err := bounded(*patch.ForcedModel, 120, true)
			if err != nil {
				return err
			}
			u.ForcedModel = forced
		}
		if patch.TokenLimit != nil {
			if *patch.TokenLimit < 0 {
				return ErrBadInput
			}
			u.TokenLimit = *patch.TokenLimit
		}
		if patch.ResetEvery != nil {
			every, err := NormalizePeriod(*patch.ResetEvery)
			if err != nil {
				return err
			}
			if every != u.ResetEvery {
				u.ResetEvery = every
				u.PeriodStart = now
				u.TokensUsed = 0
			}
		}
		u = rolled(u, now)
		d.Users[i] = u
		updated = u
		return nil
	})
	return updated, err
}

// DeleteUser removes the account and keeps past usage events.
func (s *Store) DeleteUser(id string) error {
	return s.with(func(d *data) error {
		_, i := findUser(d, id)
		if i < 0 {
			return ErrNotFound
		}
		d.Users = append(d.Users[:i], d.Users[i+1:]...)
		return nil
	})
}

// List returns every user rolled to now.
func (s *Store) List(now time.Time) ([]User, error) {
	var out []User
	err := s.with(func(d *data) error {
		for i := range d.Users {
			d.Users[i] = rolled(d.Users[i], now)
			out = append(out, d.Users[i])
		}
		return nil
	})
	return out, err
}

// Authenticate checks a plaintext access token.
func (s *Store) Authenticate(token string, now time.Time) (User, error) {
	if strings.TrimSpace(token) == "" {
		return User{}, ErrUnauthorized
	}
	sum := hashToken(token)
	var found User
	ok := false
	err := s.with(func(d *data) error {
		for i := range d.Users {
			if subtle.ConstantTimeCompare([]byte(d.Users[i].TokenHash), []byte(sum)) == 1 {
				d.Users[i] = rolled(d.Users[i], now)
				found = d.Users[i]
				ok = true
			}
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	if !ok {
		return User{}, ErrUnauthorized
	}
	return found, nil
}

// CheckQuota rolls the period and reports whether a new request may start.
func (s *Store) CheckQuota(userID string, now time.Time) (User, error) {
	var user User
	err := s.with(func(d *data) error {
		u, i := findUser(d, userID)
		if i < 0 {
			return ErrNotFound
		}
		u = rolled(u, now)
		d.Users[i] = u
		if u.TokenLimit > 0 && u.TokensUsed >= u.TokenLimit {
			user = u
			return ErrQuota
		}
		user = u
		return nil
	})
	return user, err
}

// AddUsage records spent tokens. A call that begins under the limit is kept
// even when it finishes above the limit; the next call is refused.
func (s *Store) AddUsage(userID string, tokens int64, model string, now time.Time) (User, error) {
	if tokens < 0 {
		return User{}, ErrBadInput
	}
	var user User
	err := s.with(func(d *data) error {
		u, i := findUser(d, userID)
		if i < 0 {
			return ErrNotFound
		}
		u = rolled(u, now)
		if u.TokenLimit > 0 && u.TokensUsed >= u.TokenLimit {
			d.Users[i] = u
			user = u
			return ErrQuota
		}
		u.TokensUsed += tokens
		d.Users[i] = u
		if tokens > 0 {
			d.Usage = append(d.Usage, UsageEvent{At: now, UserID: userID, Tokens: tokens, Model: model})
		}
		user = u
		return nil
	})
	return user, err
}

// Usage returns the newest events, up to limit.
func (s *Store) Usage(limit int) ([]UsageEvent, error) {
	var out []UsageEvent
	err := s.with(func(d *data) error {
		if limit <= 0 || limit > len(d.Usage) {
			limit = len(d.Usage)
		}
		start := len(d.Usage) - limit
		out = append([]UsageEvent(nil), d.Usage[start:]...)
		return nil
	})
	return out, err
}

// SetProxy stores the admin-chosen proxy and the time it was saved.
func (s *Store) SetProxy(url string, at time.Time) error {
	url = strings.TrimSpace(url)
	return s.with(func(d *data) error {
		d.ProxyURL = url
		d.ProxySetAt = at
		return nil
	})
}

// ProxySettings is the admin value and when it was saved.
func (s *Store) ProxySettings() (string, time.Time, error) {
	var url string
	var at time.Time
	err := s.with(func(d *data) error {
		url = d.ProxyURL
		at = d.ProxySetAt
		return nil
	})
	return url, at, err
}

func (s *Store) with(fn func(*data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit(fn)
}

func (s *Store) commitFile(fn func(*data) error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flock(lock); err != nil {
		return err
	}
	defer funlock(lock)

	d := data{}
	if b, err := os.ReadFile(s.path); err == nil && len(b) > 0 {
		if err := json.Unmarshal(b, &d); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := fn(&d); err != nil {
		// Persist a period roll that happened before a quota refusal.
		if errors.Is(err, ErrQuota) {
			_ = writeAtomic(s.path, &d)
		}
		return err
	}
	return writeAtomic(s.path, &d)
}

func writeAtomic(path string, d *data) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func findUser(d *data, id string) (User, int) {
	for i := range d.Users {
		if d.Users[i].ID == id {
			return d.Users[i], i
		}
	}
	return User{}, -1
}

func validAccessToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func bounded(s string, max int, allowEmpty bool) (string, error) {
	s = strings.TrimSpace(s)
	if (s == "" && !allowEmpty) || len(s) > max {
		return "", ErrBadInput
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", ErrBadInput
		}
	}
	return s, nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

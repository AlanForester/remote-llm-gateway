package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	redisStateKey = "remote-llm:state"
	redisLockKey  = "remote-llm:lock"
)

// OpenRedis stores the same JSON document as the file store under one key.
// Several gateway, api, and admin processes can share it.
func OpenRedis(rawURL string) (*Store, error) {
	rawURL = strings.TrimSpace(rawURL)
	opt, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	s := &Store{commit: func(fn func(*data) error) error {
		return commitRedis(client, fn)
	}}
	if err := s.with(func(*data) error { return nil }); err != nil {
		_ = client.Close()
		return nil, err
	}
	return s, nil
}

func commitRedis(client *redis.Client, fn func(*data) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token := newID()
	if err := lockRedis(ctx, client, token); err != nil {
		return err
	}
	defer unlockRedis(client, token)

	d := data{}
	raw, err := client.Get(ctx, redisStateKey).Bytes()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
	}
	if err := fn(&d); err != nil {
		if errors.Is(err, ErrQuota) {
			_ = saveRedis(ctx, client, &d)
		}
		return err
	}
	return saveRedis(ctx, client, &d)
}

func lockRedis(ctx context.Context, client *redis.Client, token string) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok, err := client.SetNX(ctx, redisLockKey, token, 5*time.Second).Result()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("redis lock timeout")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func unlockRedis(client *redis.Client, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	const script = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	_ = client.Eval(ctx, script, []string{redisLockKey}, token).Err()
}

func saveRedis(ctx context.Context, client *redis.Client, d *data) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return client.Set(ctx, redisStateKey, b, 0).Err()
}

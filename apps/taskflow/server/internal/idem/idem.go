package idem

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	firePrefix = "taskflow:idem:fire:"
	evtPrefix  = "taskflow:idem:evt:"
)

type Service struct {
	client redis.UniversalClient
	ttl    time.Duration
}

func New(client redis.UniversalClient, ttl time.Duration) *Service {
	return &Service{client: client, ttl: ttl}
}

func (s *Service) ClaimFire(ctx context.Context, fireKey string) (bool, error) {
	return s.claim(ctx, firePrefix+fireKey)
}

func (s *Service) ClaimEvent(ctx context.Context, fireKey string) (bool, error) {
	return s.claim(ctx, evtPrefix+fireKey)
}

func (s *Service) ReleaseFire(ctx context.Context, fireKey string) error {
	return s.client.Del(ctx, firePrefix+fireKey).Err()
}

func (s *Service) ReleaseEvent(ctx context.Context, fireKey string) error {
	return s.client.Del(ctx, evtPrefix+fireKey).Err()
}

func (s *Service) claim(ctx context.Context, key string) (bool, error) {
	ok, err := s.client.SetNX(ctx, key, time.Now().UnixNano(), s.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("idem claim %s: %w", key, err)
	}
	return ok, nil
}

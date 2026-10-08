// Package idem provides the redis fast-path layer of the idempotency design.
// Every trigger (scheduled fire, condition event, manual run) carries a
// deterministic fire key; Claim succeeds exactly once per key within the TTL
// window. The durable layer is the unique index on executions.fire_key, so a
// lost redis claim can never cause a second execution row.
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

// ClaimFire attempts the fire-key claim. ok=false means somebody else already
// owns this fire (duplicate trigger).
func (s *Service) ClaimFire(ctx context.Context, fireKey string) (bool, error) {
	return s.claim(ctx, firePrefix+fireKey)
}

// ClaimEvent deduplicates condition-event ingestion before an execution row is
// even created.
func (s *Service) ClaimEvent(ctx context.Context, fireKey string) (bool, error) {
	return s.claim(ctx, evtPrefix+fireKey)
}

// ReleaseFire drops a fire claim, used when the dispatch transaction cancels
// so that a later recovery pass can re-dispatch the same fire key.
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

package cache

import "context"

type Cache interface {
	Get(ctx context.Context, key string) (string, bool)
	Set(ctx context.Context, key string, value string) error
	Delete(ctx context.Context, key string) error
}

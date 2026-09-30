
package main

import (
	"context"

	yukino_cache "github.com/hangtiancheng/yukino.go/yukino_cache"
)

type cache struct {
	group *yukino_cache.Group
}

func newCache() *cache {
	return &cache{group: yukino_cache.NewGroup("yukino_chatbot", 64<<20, yukino_cache.GetterFunc(func(ctx context.Context, key string) ([]byte, error) {
		return nil, yukino_cache.ErrKeyRequired
	}))}
}

func (c *cache) Get(ctx context.Context, key string) (string, bool) {
	view, err := c.group.Get(ctx, key)
	if err != nil {
		return "", false
	}
	return view.String(), true
}

func (c *cache) Set(ctx context.Context, key string, value string) error {
	return c.group.Set(ctx, key, []byte(value))
}

func (c *cache) Delete(ctx context.Context, key string) error {
	return c.group.Delete(ctx, key)
}

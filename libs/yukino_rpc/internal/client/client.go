package client

import (
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/limiter"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/load_balance"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/registry"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/transport"
)

type Client struct {
	reg       *registry.Registry
	lb        load_balance.LoadBalancer
	limiter   *limiter.TokenBucket
	timeout   time.Duration
	codec     codec.Codec
	codecType codec.Type
	breaker   sync.Map

	pools sync.Map
}

func NewClient(reg *registry.Registry, opts ...ClientOption) (*Client, error) {
	cc, err := codec.New(codec.JSON)
	if err != nil {
		return nil, err
	}

	c := &Client{
		reg:       reg,
		lb:        &load_balance.RoundRobin{},
		limiter:   limiter.NewTokenBucket(10000),
		timeout:   5 * time.Second,
		codec:     cc,
		codecType: codec.JSON,
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *Client) Close() {
	c.limiter.Stop()
	c.pools.Range(func(key, value any) bool {
		pool := value.(*transport.ConnectionPool)
		pool.Close()
		return true
	})
}

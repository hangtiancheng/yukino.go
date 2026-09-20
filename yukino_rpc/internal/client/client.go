// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package client

import (
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/limiter"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/load_balance"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/registry"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/transport"
)

type Client struct {
	reg       *registry.Registry
	lb        load_balance.LoadBalancer
	limiter   *limiter.TokenBucket
	timeout   time.Duration
	codec     codec.Codec
	codecType codec.Type
	breaker   sync.Map // map[string]*CircuitBreaker

	pools sync.Map // map[string]*transport.ConnectionPool
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

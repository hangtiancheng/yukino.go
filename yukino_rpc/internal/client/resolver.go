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
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/breaker"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/transport"
)

func (c *Client) getPool(addr string) *transport.ConnectionPool {
	if pool, ok := c.pools.Load(addr); ok {
		return pool.(*transport.ConnectionPool)
	}

	newPool := transport.NewConnectionPool(addr, 0, 1)
	actual, _ := c.pools.LoadOrStore(addr, newPool)
	return actual.(*transport.ConnectionPool)
}

func (c *Client) getAddr(service string) (string, error) {
	if c.reg == nil {
		return "", errors.New("registry not configured")
	}

	instances, err := c.reg.Discover(service)
	if err != nil {
		return "", err
	}

	if len(instances) == 0 {
		return "", errors.New("no instance available")
	}

	instance := c.lb.Select(instances)
	if instance.Addr == "" {
		return "", errors.New("load balancer returned empty address")
	}
	return instance.Addr, nil
}

func (c *Client) getBreaker(service, addr string) *breaker.CircuitBreaker {
	key := service + "|" + addr
	if val, ok := c.breaker.Load(key); ok {
		return val.(*breaker.CircuitBreaker)
	}

	newBreaker := breaker.NewCircuitBreaker(
		10,
		0.6,           // 60% 错误率熔断
		5*time.Second, // 熔断 5 秒
	)
	actual, _ := c.breaker.LoadOrStore(key, newBreaker)
	return actual.(*breaker.CircuitBreaker)
}

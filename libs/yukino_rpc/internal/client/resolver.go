package client

import (
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/breaker"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/transport"
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
		0.6,
		5*time.Second,
	)
	actual, _ := c.breaker.LoadOrStore(key, newBreaker)
	return actual.(*breaker.CircuitBreaker)
}

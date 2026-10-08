package client

import (
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/load_balance"
)

type ClientOption func(*Client) error

func WithClientCodec(t codec.Type) ClientOption {
	return func(c *Client) error {
		cc, err := codec.New(t)
		if err != nil {
			return err
		}
		c.codec = cc
		c.codecType = t
		return nil
	}
}

func WithClientTimeout(d time.Duration) ClientOption {
	return func(c *Client) error {
		if d <= 0 {
			return errors.New("client timeout must be positive")
		}
		c.timeout = d
		return nil
	}
}

func WithClientLoadBalancer(lb load_balance.LoadBalancer) ClientOption {
	return func(c *Client) error {
		if lb == nil {
			return errors.New("client load balancer must not be nil")
		}
		c.lb = lb
		return nil
	}
}

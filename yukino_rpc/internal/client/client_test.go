package client

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/registry"
)

func TestNewClientUsesDefaultCodec(t *testing.T) {
	c, err := NewClient(nil)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	body, err := c.codec.Marshal(map[string]int{"v": 1})
	if err != nil {
		t.Fatalf("default codec Marshal returned error: %v", err)
	}
	if string(body) != `{"v":1}` {
		t.Fatalf("body = %q, want JSON", body)
	}
}

func TestClientOptionValidation(t *testing.T) {
	if _, err := NewClient(nil, WithClientCodec(codec.Type(99))); err == nil {
		t.Fatal("expected invalid codec error")
	}
	if _, err := NewClient(nil, WithClientTimeout(0)); err == nil {
		t.Fatal("expected invalid timeout error")
	}
	if _, err := NewClient(nil, WithClientLoadBalancer(nil)); err == nil {
		t.Fatal("expected nil load balancer error")
	}
}

func TestInvokeAsyncWithoutRegistryReturnsError(t *testing.T) {
	c, err := NewClient(nil)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	if _, err := c.InvokeAsync(context.Background(), "srv", "Method", &registry.Instance{}); err == nil {
		t.Fatal("expected missing registry error")
	}
}

func TestWithClientTimeoutAcceptsPositiveDuration(t *testing.T) {
	c, err := NewClient(nil, WithClientTimeout(time.Millisecond))
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	if c.timeout != time.Millisecond {
		t.Fatalf("timeout = %s, want %s", c.timeout, time.Millisecond)
	}
}

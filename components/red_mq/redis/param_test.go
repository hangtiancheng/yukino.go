package redis

import (
	"context"
	"testing"
	"time"
)

// unreachableAddr is a port where nothing listens, so every network call
// fails fast without touching a real redis.
const unreachableAddr = "127.0.0.1:1"

func newLocalTestClient(t *testing.T) *Client {
	t.Helper()
	c := NewClient("tcp", unreachableAddr, "")
	t.Cleanup(func() {
		c.Close()
	})
	return c
}

func TestNewClientKeepsOptions(t *testing.T) {
	c := NewClient("tcp", unreachableAddr, "",
		WithMaxIdle(3),
		WithMaxActive(5),
		WithIdleTimeoutSeconds(7),
	)
	defer c.Close()

	if c.opts == nil {
		t.Fatal("NewClient dropped the client options")
	}
	if c.opts.maxIdle != 3 || c.opts.maxActive != 5 || c.opts.idleTimeoutSeconds != 7 {
		t.Fatalf("opts = %+v, want maxIdle=3 maxActive=5 idleTimeoutSeconds=7", c.opts)
	}
}

func TestCloseIdempotentEnough(t *testing.T) {
	c := newLocalTestClient(t)
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// A second Close reports the client as closed instead of panicking.
	if err := c.Close(); err != nil && err.Error() != "redis: client is closed" {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCommandParamGuards(t *testing.T) {
	c := newLocalTestClient(t)
	ctx := context.Background()

	if _, err := c.XADD(ctx, "", 0, "k", "v"); err == nil {
		t.Error("XADD with empty topic must fail")
	}
	if err := c.XACK(ctx, "", "g", "1-1"); err == nil {
		t.Error("XACK with empty topic must fail")
	}
	if err := c.XACK(ctx, "t", "g", ""); err == nil {
		t.Error("XACK with empty msg id must fail")
	}
	if _, err := c.XReadGroup(ctx, "", "c", "t", 0); err == nil {
		t.Error("XReadGroup with empty group must fail")
	}
	if _, err := c.XReadGroupPending(ctx, "g", "", "t"); err == nil {
		t.Error("XReadGroupPending with empty consumer must fail")
	}
	if _, err := c.Get(ctx, ""); err == nil {
		t.Error("Get with empty key must fail")
	}
	if _, err := c.Set(ctx, "k", ""); err == nil {
		t.Error("Set with empty value must fail")
	}
	if _, err := c.SetNEX(ctx, "", "v", 1); err == nil {
		t.Error("SetNEX with empty key must fail")
	}
	if _, err := c.SetNX(ctx, "k", ""); err == nil {
		t.Error("SetNX with empty value must fail")
	}
	if err := c.Del(ctx, ""); err == nil {
		t.Error("Del with empty key must fail")
	}
	if _, err := c.Incr(ctx, ""); err == nil {
		t.Error("Incr with empty key must fail")
	}
}

// TestEvalKeyCountBounds guards the make() calls in Eval: a keyCount beyond
// the supplied args (or negative) used to panic with a negative capacity.
func TestEvalKeyCountBounds(t *testing.T) {
	c := newLocalTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	if _, err := c.Eval(ctx, "return 1", 3, []any{"k1"}); err == nil {
		t.Error("Eval with keyCount > len(args) must fail, not panic")
	}
	if _, err := c.Eval(ctx, "return 1", -1, []any{"k1", "a"}); err == nil {
		t.Error("Eval with negative keyCount must fail, not panic")
	}
	if _, err := c.Eval(ctx, "return 1", 1, nil); err == nil {
		t.Error("Eval with keyCount on nil args must fail, not panic")
	}
}

func TestParseStreamValues(t *testing.T) {
	key, val := parseStreamValues(map[string]any{"key": "val"})
	if key != "key" || val != "val" {
		t.Fatalf("got (%q, %q), want (key, val)", key, val)
	}

	key, val = parseStreamValues(map[string]any{"key": []byte("val")})
	if key != "key" || val != "val" {
		t.Fatalf("byte values: got (%q, %q), want (key, val)", key, val)
	}

	key, val = parseStreamValues(nil)
	if key != "" || val != "" {
		t.Fatalf("nil values: got (%q, %q), want empty", key, val)
	}
}

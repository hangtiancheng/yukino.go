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

package redis

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/consistent_cache"
)

// fakeClient records the commands issued by Cache.
type fakeClient struct {
	mu sync.Mutex

	getVal string
	getErr error
	setErr error

	setKeys   []string
	setValues []string
	setExps   []int64

	pexpireKeys   []string
	pexpireMillis []int64

	evalSrc      string
	evalKeyCount int
	evalKeys     []string
	evalArgs     []any
	evalReply    any
	evalErr      error
}

func (f *fakeClient) Eval(_ context.Context, src string, keyCount int, keysAndArgs []any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evalSrc = src
	f.evalKeyCount = keyCount
	for i, v := range keysAndArgs {
		if i < keyCount {
			f.evalKeys = append(f.evalKeys, v.(string))
		} else {
			f.evalArgs = append(f.evalArgs, v)
		}
	}
	return f.evalReply, f.evalErr
}

func (f *fakeClient) Get(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getVal, f.getErr
}

func (f *fakeClient) SetEx(_ context.Context, key, value string, expireSeconds int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setKeys = append(f.setKeys, key)
	f.setValues = append(f.setValues, value)
	f.setExps = append(f.setExps, expireSeconds)
	return f.setErr
}

func (f *fakeClient) Del(_ context.Context, _ string) error {
	return nil
}

func (f *fakeClient) PExpire(_ context.Context, key string, expireMillis int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pexpireKeys = append(f.pexpireKeys, key)
	f.pexpireMillis = append(f.pexpireMillis, expireMillis)
	return nil
}

func TestCacheEnableExpiresDisableMarker(t *testing.T) {
	fc := &fakeClient{}
	c := &Cache{client: fc}

	if err := c.Enable(context.Background(), "user:1", 500); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if len(fc.pexpireKeys) != 1 {
		t.Fatalf("PExpire called %d times, want 1", len(fc.pexpireKeys))
	}
	// Enable must target the disable marker, not the data key: the data key
	// is deleted right before the db write, so expiring it would be a no-op
	// and the read path would stay disabled for the full marker TTL.
	if fc.pexpireKeys[0] != "Enable_Lock_Key_{user:1}" {
		t.Fatalf("PExpire key = %q, want the disable marker %q", fc.pexpireKeys[0], "Enable_Lock_Key_{user:1}")
	}
	if fc.pexpireMillis[0] != 500 {
		t.Fatalf("PExpire millis = %d, want 500", fc.pexpireMillis[0])
	}
}

func TestCacheDisableUsesDisableMarker(t *testing.T) {
	fc := &fakeClient{}
	c := &Cache{client: fc}

	if err := c.Disable(context.Background(), "user:1", 10); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if len(fc.setKeys) != 1 {
		t.Fatalf("SetEx called %d times, want 1", len(fc.setKeys))
	}
	if fc.setKeys[0] != "Enable_Lock_Key_{user:1}" {
		t.Fatalf("SetEx key = %q, want the disable marker", fc.setKeys[0])
	}
	if fc.setValues[0] != "1" || fc.setExps[0] != 10 {
		t.Fatalf("SetEx value/expire = %q/%d, want %q/%d", fc.setValues[0], fc.setExps[0], "1", 10)
	}
}

func TestCacheGetTranslatesMiss(t *testing.T) {
	ctx := context.Background()

	// Cache miss is translated to the consistent_cache sentinel.
	c := &Cache{client: &fakeClient{getErr: ErrorCacheMiss}}
	if _, err := c.Get(ctx, "k"); !errors.Is(err, consistent_cache.ErrorCacheMiss) {
		t.Fatalf("err = %v, want consistent_cache.ErrorCacheMiss", err)
	}

	// A hit is passed through.
	c = &Cache{client: &fakeClient{getVal: "v"}}
	v, err := c.Get(ctx, "k")
	if err != nil || v != "v" {
		t.Fatalf("Get = %q, %v, want %q, nil", v, err, "v")
	}

	// Other errors are passed through unchanged.
	boom := errors.New("boom")
	c = &Cache{client: &fakeClient{getErr: boom}}
	if _, err := c.Get(ctx, "k"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestCachePutWhenEnable(t *testing.T) {
	ctx := context.Background()

	t.Run("enabled writes", func(t *testing.T) {
		fc := &fakeClient{evalReply: int64(1)}
		c := &Cache{client: fc}
		ok, err := c.PutWhenEnable(ctx, "k", "v", 60)
		if err != nil {
			t.Fatalf("PutWhenEnable: %v", err)
		}
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if fc.evalSrc != LuaCheckEnableAndWriteCache {
			t.Fatal("unexpected lua script")
		}
		if fc.evalKeyCount != 2 {
			t.Fatalf("keyCount = %d, want 2", fc.evalKeyCount)
		}
		if len(fc.evalKeys) != 2 || fc.evalKeys[0] != "Enable_Lock_Key_{k}" || fc.evalKeys[1] != "k" {
			t.Fatalf("keys = %v, want [Enable_Lock_Key_{k} k]", fc.evalKeys)
		}
		if len(fc.evalArgs) != 2 || fc.evalArgs[0] != "v" || fc.evalArgs[1] != int64(60) {
			t.Fatalf("args = %v, want [v 60]", fc.evalArgs)
		}
	})

	t.Run("disabled suppresses", func(t *testing.T) {
		fc := &fakeClient{evalReply: int64(0)}
		c := &Cache{client: fc}
		ok, err := c.PutWhenEnable(ctx, "k", "v", 60)
		if err != nil {
			t.Fatalf("PutWhenEnable: %v", err)
		}
		if ok {
			t.Fatal("ok = true, want false")
		}
	})

	t.Run("unexpected reply type", func(t *testing.T) {
		fc := &fakeClient{evalReply: "1"}
		c := &Cache{client: fc}
		if _, err := c.PutWhenEnable(ctx, "k", "v", 60); err == nil {
			t.Fatal("expected an error for a non-int reply")
		}
	})

	t.Run("eval error", func(t *testing.T) {
		boom := errors.New("boom")
		fc := &fakeClient{evalErr: boom}
		c := &Cache{client: fc}
		if _, err := c.PutWhenEnable(ctx, "k", "v", 60); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want boom", err)
		}
	})
}

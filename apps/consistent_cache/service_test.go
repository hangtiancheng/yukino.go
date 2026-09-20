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

package consistent_cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// nopLogger silences service diagnostics during tests.
type nopLogger struct{}

func (nopLogger) Errorf(format string, v ...any) {}
func (nopLogger) Warnf(format string, v ...any)  {}
func (nopLogger) Infof(format string, v ...any)  {}
func (nopLogger) Debugf(format string, v ...any) {}

// fakeCache models the redis semantics: a value store plus a disable-marker set.
type fakeCache struct {
	mu     sync.Mutex
	values map[string]string
	// disable holds the keys whose read-path write cache is currently disabled.
	disable map[string]bool

	getErr     error
	disableErr error
	putErr     error

	// enableCh records every Enable call; the async re-enable in Service.Put
	// sends here, so tests can wait for it deterministically.
	enableCh chan string
}

func newFakeCache() *fakeCache {
	return &fakeCache{
		values:   make(map[string]string),
		disable:  make(map[string]bool),
		enableCh: make(chan string, 4096),
	}
}

func (c *fakeCache) Enable(_ context.Context, key string, _ int64) error {
	c.mu.Lock()
	delete(c.disable, key)
	c.mu.Unlock()
	c.enableCh <- key
	return nil
}

func (c *fakeCache) Disable(_ context.Context, key string, _ int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disableErr != nil {
		return c.disableErr
	}
	c.disable[key] = true
	return nil
}

func (c *fakeCache) Get(_ context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.getErr != nil {
		return "", c.getErr
	}
	v, ok := c.values[key]
	if !ok {
		return "", ErrorCacheMiss
	}
	return v, nil
}

func (c *fakeCache) Del(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, key)
	return nil
}

func (c *fakeCache) PutWhenEnable(_ context.Context, key, value string, _ int64) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.putErr != nil {
		return false, c.putErr
	}
	if c.disable[key] {
		return false, nil
	}
	c.values[key] = value
	return true, nil
}

func (c *fakeCache) value(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.values[key]
	return v, ok
}

// fakeDB is a simple key/value DB stub.
type fakeDB struct {
	mu     sync.Mutex
	rows   map[string]string
	getErr error
	putErr error
	puts   int
}

func (d *fakeDB) Put(_ context.Context, obj Object) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.putErr != nil {
		return d.putErr
	}
	v, err := obj.Write()
	if err != nil {
		return err
	}
	d.rows[obj.Key()] = v
	d.puts++
	return nil
}

func (d *fakeDB) Get(_ context.Context, obj Object) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.getErr != nil {
		return d.getErr
	}
	v, ok := d.rows[obj.Key()]
	if !ok {
		return ErrorDBMiss
	}
	return obj.Read(v)
}

// testObject is a minimal Object implementation.
type testObject struct {
	key  string
	data string
}

func (o *testObject) KeyColumn() string      { return "key" }
func (o *testObject) Key() string            { return o.key }
func (o *testObject) Write() (string, error) { return o.data, nil }
func (o *testObject) Read(body string) error { o.data = body; return nil }

func newTestService(cache Cache, db DB) *Service {
	return NewService(cache, db, WithLogger(nopLogger{}))
}

// waitForEnable waits for the async Enable call issued by Service.Put.
func waitForEnable(t *testing.T, c *fakeCache) string {
	t.Helper()
	select {
	case key := <-c.enableCh:
		return key
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the async Enable call")
		return ""
	}
}

func TestServicePutThenGet(t *testing.T) {
	cache := newFakeCache()
	db := &fakeDB{rows: make(map[string]string)}
	s := newTestService(cache, db)
	ctx := context.Background()

	if err := s.Put(ctx, &testObject{key: "k1", data: "v1"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := db.rows["k1"]; got != "v1" {
		t.Fatalf("db row = %q, want %q", got, "v1")
	}
	if v, ok := cache.value("k1"); ok {
		t.Fatalf("cache should be empty after Put, got %q", v)
	}
	if key := waitForEnable(t, cache); key != "k1" {
		t.Fatalf("Enable called with key %q, want %q", key, "k1")
	}

	// First Get: cache miss -> db -> backfill.
	r1 := &testObject{key: "k1"}
	useCache, err := s.Get(ctx, r1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if useCache {
		t.Fatal("first Get should miss the cache")
	}
	if r1.data != "v1" {
		t.Fatalf("data = %q, want %q", r1.data, "v1")
	}
	if v, ok := cache.value("k1"); !ok || v != "v1" {
		t.Fatalf("cache backfill missing, got %q ok=%t", v, ok)
	}

	// Second Get: served from cache.
	r2 := &testObject{key: "k1"}
	useCache, err = s.Get(ctx, r2)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !useCache {
		t.Fatal("second Get should hit the cache")
	}
	if r2.data != "v1" {
		t.Fatalf("data = %q, want %q", r2.data, "v1")
	}
}

func TestServiceGetNullDataAntiPenetration(t *testing.T) {
	cache := newFakeCache()
	db := &fakeDB{rows: make(map[string]string)} // empty table
	s := newTestService(cache, db)
	ctx := context.Background()

	obj := &testObject{key: "missing"}
	useCache, err := s.Get(ctx, obj)
	if !errors.Is(err, ErrorDataNotExist) {
		t.Fatalf("err = %v, want ErrorDataNotExist", err)
	}
	if useCache {
		t.Fatal("first Get should not use cache")
	}
	if v, ok := cache.value("missing"); !ok || v != NullData {
		t.Fatalf("null sentinel not cached, got %q ok=%t", v, ok)
	}

	// The second read is served by the negative cache.
	useCache, err = s.Get(ctx, &testObject{key: "missing"})
	if !errors.Is(err, ErrorDataNotExist) {
		t.Fatalf("err = %v, want ErrorDataNotExist", err)
	}
	if !useCache {
		t.Fatal("second Get should be served from the negative cache")
	}
}

func TestServiceGetSuppressedWhileDisabled(t *testing.T) {
	cache := newFakeCache()
	db := &fakeDB{rows: map[string]string{"k": "v"}}
	s := newTestService(cache, db)
	ctx := context.Background()

	// Simulate an in-flight write window: marker present, Enable not yet called.
	if err := cache.Disable(ctx, "k", 10); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	obj := &testObject{key: "k"}
	useCache, err := s.Get(ctx, obj)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if useCache {
		t.Fatal("expected a db read during the disable window")
	}
	if obj.data != "v" {
		t.Fatalf("data = %q, want %q", obj.data, "v")
	}
	if v, ok := cache.value("k"); ok {
		t.Fatalf("cache must stay empty while disabled, got %q", v)
	}

	// Re-enable (marker expires) and read again: this time the backfill lands.
	if err := cache.Enable(ctx, "k", 0); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if _, err = s.Get(ctx, &testObject{key: "k"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, ok := cache.value("k"); !ok || v != "v" {
		t.Fatalf("cache backfill missing after re-enable, got %q ok=%t", v, ok)
	}
}

func TestServiceGetPropagatesErrors(t *testing.T) {
	boom := errors.New("boom")

	// A cache error other than a miss is returned as-is.
	s := newTestService(&fakeCache{getErr: boom}, &fakeDB{rows: map[string]string{}})
	if _, err := s.Get(context.Background(), &testObject{key: "k"}); !errors.Is(err, boom) {
		t.Fatalf("cache err = %v, want boom", err)
	}

	// A db error other than a miss is returned as-is.
	s = newTestService(newFakeCache(), &fakeDB{rows: map[string]string{}, getErr: boom})
	if _, err := s.Get(context.Background(), &testObject{key: "k"}); !errors.Is(err, boom) {
		t.Fatalf("db err = %v, want boom", err)
	}
}

func TestServicePutDisableFail(t *testing.T) {
	boom := errors.New("disable boom")
	cache := newFakeCache()
	cache.disableErr = boom
	db := &fakeDB{rows: map[string]string{}}
	s := newTestService(cache, db)

	if err := s.Put(context.Background(), &testObject{key: "k"}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if db.puts != 0 {
		t.Fatalf("db.Put called %d times, want 0", db.puts)
	}
	// The deferred re-enable must not have been scheduled.
	select {
	case key := <-cache.enableCh:
		t.Fatalf("Enable unexpectedly called for key %q", key)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestCacheExpireSecondsConcurrent guards the mutex around randInst: with
// WithCacheExpireRandomMode, CacheExpireSeconds is called concurrently by
// Service.Get, and math/rand sources are not safe for concurrent use.
// Run with -race.
func TestCacheExpireSecondsConcurrent(t *testing.T) {
	s := NewService(newFakeCache(), &fakeDB{rows: map[string]string{}},
		WithLogger(nopLogger{}),
		WithCacheExpireSeconds(100),
		WithCacheExpireRandomMode(),
	)

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 500 {
				v := s.opts.CacheExpireSeconds()
				if v < 100 || v > 200 {
					t.Errorf("CacheExpireSeconds() = %d, want in [100, 200]", v)
					return
				}
			}
		})
	}
	wg.Wait()
}

// TestServiceConcurrentPutGet runs the full Put/Get flow through the public
// API with many goroutines on one shared Service. It validates (under -race)
// that the service and its options are safe for concurrent use.
func TestServiceConcurrentPutGet(t *testing.T) {
	cache := newFakeCache()
	db := &fakeDB{rows: map[string]string{"hot": "v0"}}
	s := NewService(cache, db,
		WithLogger(nopLogger{}),
		WithCacheExpireSeconds(60),
		WithCacheExpireRandomMode(),
	)
	ctx := context.Background()

	const writers = 8
	const readers = 8
	const rounds = 40

	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			for j := range rounds {
				obj := &testObject{key: "hot", data: fmt.Sprintf("w%d-%d", i, j)}
				if err := s.Put(ctx, obj); err != nil {
					t.Errorf("Put: %v", err)
					return
				}
			}
		})
	}
	for range readers {
		wg.Go(func() {
			for range rounds {
				obj := &testObject{key: "hot"}
				if _, err := s.Get(ctx, obj); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
			}
		})
	}
	wg.Wait()
}

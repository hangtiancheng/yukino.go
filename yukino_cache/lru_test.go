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

package yukino_cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type testValue string

func (v testValue) Len() int { return len(v) }

func TestNewOptionsAndNewStore(t *testing.T) {
	opts := NewStoreOptions()
	if opts.MaxBytes <= 0 || opts.BucketCount == 0 || opts.CapPerBucket == 0 || opts.Level2Cap == 0 {
		t.Fatalf("unexpected default options: %+v", opts)
	}

	stores := []*lruStore{
		NewStore(StoreOptions{MaxBytes: 64, CleanupInterval: time.Hour}),
		NewStore(StoreOptions{BucketCount: 1, CapPerBucket: 2, Level2Cap: 2, CleanupInterval: time.Hour}),
		NewStore(StoreOptions{MaxBytes: 64, CleanupInterval: time.Hour}),
	}
	for _, s := range stores {
		if s == nil {
			t.Fatal("NewStore returned nil")
		}
		s.Close()
	}
}

func TestLRUCacheBasicOperations(t *testing.T) {
	var evicted []string
	c := NewStore(StoreOptions{
		BucketCount:     1,
		CapPerBucket:    2,
		Level2Cap:       2,
		CleanupInterval: time.Hour,
		OnEvicted: func(key string, value Value) {
			evicted = append(evicted, key)
		},
	})
	defer c.Close()

	if _, ok := c.Get("missing"); ok {
		t.Fatal("missing key returned a hit")
	}

	if err := c.Set("a", testValue("alpha")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := c.Set("b", testValue("beta")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got, ok := c.Get("a"); !ok || got != testValue("alpha") {
		t.Fatalf("Get(a) = %v, %v", got, ok)
	}
	if got, ok := c.Get("b"); !ok || got != testValue("beta") {
		t.Fatalf("Get(b) = %v, %v", got, ok)
	}

	if !c.Delete("a") {
		t.Fatal("Delete(a) returned false")
	}
	if c.Delete("missing") {
		t.Fatal("Delete(missing) returned true")
	}
	c.Clear()
	if c.Len() != 0 {
		t.Fatalf("cache not empty after Clear: len=%d", c.Len())
	}
}

func TestLRUCacheCapacityEviction(t *testing.T) {
	var evicted []string
	c := NewStore(StoreOptions{
		BucketCount:     1,
		CapPerBucket:    2,
		Level2Cap:       2,
		CleanupInterval: time.Hour,
		OnEvicted: func(key string, value Value) {
			evicted = append(evicted, key)
		},
	})
	defer c.Close()

	if err := c.Set("a", testValue("alpha")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := c.Set("b", testValue("beta")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if err := c.Set("c", testValue("gamma")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}

	if _, ok := c.Get("a"); ok {
		t.Fatal("oldest key a should have been evicted when bucket overflowed")
	}
	if got, ok := c.Get("b"); !ok || got != testValue("beta") {
		t.Fatalf("Get(b) = %v, %v", got, ok)
	}
	if got, ok := c.Get("c"); !ok || got != testValue("gamma") {
		t.Fatalf("Get(c) = %v, %v", got, ok)
	}
	if len(evicted) == 0 || evicted[0] != "a" {
		t.Fatalf("expected eviction of a, got %v", evicted)
	}
}

func TestLRUCacheExpiration(t *testing.T) {
	c := NewStore(StoreOptions{BucketCount: 1, CapPerBucket: 8, Level2Cap: 8, CleanupInterval: 50 * time.Millisecond})
	defer c.Close()

	if err := c.SetWithExpiration("short", testValue("value"), 150*time.Millisecond); err != nil {
		t.Fatalf("SetWithExpiration returned error: %v", err)
	}
	if got, ok := c.Get("short"); !ok || got != testValue("value") {
		t.Fatalf("Get(short) before expiration = %v, %v", got, ok)
	}

	time.Sleep(400 * time.Millisecond)
	if _, ok := c.Get("short"); ok {
		t.Fatal("expired key returned a hit")
	}

	c.Close()
	c.Close()
}

func TestLRUStoreBasicOperations(t *testing.T) {
	var evicted []string
	s := NewStore(StoreOptions{
		BucketCount:     1,
		CapPerBucket:    2,
		Level2Cap:       2,
		CleanupInterval: time.Hour,
		OnEvicted: func(key string, value Value) {
			evicted = append(evicted, key)
		},
	})
	defer s.Close()

	if err := s.Set("a", testValue("alpha")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	if got, ok := s.Get("a"); !ok || got != testValue("alpha") {
		t.Fatalf("Get(a) = %v, %v", got, ok)
	}
	if got, ok := s.Get("a"); !ok || got != testValue("alpha") {
		t.Fatalf("second Get(a) = %v, %v", got, ok)
	}

	if err := s.Set("a", testValue("updated")); err != nil {
		t.Fatalf("update returned error: %v", err)
	}
	if got, ok := s.Get("a"); !ok || got != testValue("updated") {
		t.Fatalf("updated Get(a) = %v, %v", got, ok)
	}

	if !s.Delete("a") {
		t.Fatal("Delete(a) returned false")
	}
	if s.Delete("missing") {
		t.Fatal("Delete(missing) returned true")
	}
	if _, ok := s.Get("a"); ok {
		t.Fatal("deleted key returned a hit")
	}
	if len(evicted) == 0 || evicted[len(evicted)-1] != "a" {
		t.Fatalf("expected delete eviction for a, got %v", evicted)
	}
}

func TestLRUStorePromotionAndEviction(t *testing.T) {
	s := NewStore(StoreOptions{BucketCount: 1, CapPerBucket: 2, Level2Cap: 2, CleanupInterval: time.Hour})
	defer s.Close()

	for _, key := range []string{"a", "b"} {
		if err := s.Set(key, testValue("value-"+key)); err != nil {
			t.Fatalf("Set(%s) returned error: %v", key, err)
		}
		if _, ok := s.Get(key); !ok {
			t.Fatalf("Get(%s) should promote key to level 2", key)
		}
	}
	if err := s.Set("c", testValue("value-c")); err != nil {
		t.Fatalf("Set(c) returned error: %v", err)
	}
	if err := s.Set("d", testValue("value-d")); err != nil {
		t.Fatalf("Set(d) returned error: %v", err)
	}
	if _, ok := s.Get("a"); !ok {
		t.Fatal("promoted key a should still be in level 2")
	}
	if _, ok := s.Get("c"); !ok {
		t.Fatal("newer level 1 key c should still exist")
	}
}

func TestLRUStoreExpirationCleanupAndClear(t *testing.T) {
	s := NewStore(StoreOptions{BucketCount: 2, CapPerBucket: 4, Level2Cap: 4, CleanupInterval: 50 * time.Millisecond})
	defer s.Close()

	if err := s.SetWithExpiration("soon", testValue("value"), 150*time.Millisecond); err != nil {
		t.Fatalf("SetWithExpiration returned error: %v", err)
	}
	if err := s.SetWithExpiration("later", testValue("value"), time.Hour); err != nil {
		t.Fatalf("SetWithExpiration returned error: %v", err)
	}
	if _, ok := s.Get("soon"); !ok {
		t.Fatal("soon should be found before expiration")
	}
	time.Sleep(400 * time.Millisecond)
	if _, ok := s.Get("soon"); ok {
		t.Fatal("soon should have expired")
	}
	if _, ok := s.Get("later"); !ok {
		t.Fatal("later should still be valid")
	}

	for i := range 5 {
		if err := s.Set(fmt.Sprintf("key-%d", i), testValue("value")); err != nil {
			t.Fatalf("Set returned error: %v", err)
		}
	}
	if s.Len() == 0 {
		t.Fatal("Len should be positive before Clear")
	}
	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("Len after Clear = %d, want 0", s.Len())
	}
	s.Close()
	s.Close()
}

func TestLRUHelpers(t *testing.T) {
	if MaskOfNextPowOf2(1) != 0 || MaskOfNextPowOf2(2) != 1 || MaskOfNextPowOf2(3) != 3 {
		t.Fatalf("unexpected masks: 1=%d 2=%d 3=%d", MaskOfNextPowOf2(1), MaskOfNextPowOf2(2), MaskOfNextPowOf2(3))
	}
	if HashBKRD("key") == HashBKRD("other") {
		t.Fatal("different keys should not collide in this smoke test")
	}

	c := Create(2)
	c.put("a", testValue("alpha"), Now()+int64(time.Hour), nil)
	c.put("b", testValue("beta"), Now()+int64(time.Hour), nil)
	var walked []string
	c.walk(func(key string, value Value, expireAt int64) bool {
		walked = append(walked, key)
		return len(walked) < 1
	})
	if len(walked) != 1 {
		t.Fatalf("walked %d entries, want 1", len(walked))
	}
}

func TestLRUStoreNoExpirationPersists(t *testing.T) {
	s := NewStore(StoreOptions{BucketCount: 1, CapPerBucket: 4, Level2Cap: 4, CleanupInterval: 50 * time.Millisecond})
	defer s.Close()

	if err := s.Set("permanent", testValue("value")); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if got, ok := s.Get("permanent"); !ok || got != testValue("value") {
		t.Fatalf("permanent key should not expire, got (%v, %v)", got, ok)
	}
}

func TestLRUStoreConcurrentAccess(t *testing.T) {
	s := NewStore(StoreOptions{BucketCount: 8, CapPerBucket: 64, Level2Cap: 64, CleanupInterval: time.Hour})
	defer s.Close()

	const goroutines = 8
	const operations = 100
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range operations {
				key := fmt.Sprintf("g%d-key-%d", id, i)
				if err := s.Set(key, testValue("value")); err != nil {
					t.Errorf("Set returned error: %v", err)
				}
				_, _ = s.Get(key)
				if i%2 == 0 {
					s.Delete(key)
				}
			}
		}(g)
	}
	wg.Wait()
}

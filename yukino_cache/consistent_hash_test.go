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
	"strconv"
	"testing"
	"time"
)

func TestMapAddGetRemoveAndStats(t *testing.T) {
	m := NewConsistentHash(WithConsistentHashConfig(&ConHashConfig{
		DefaultReplicas:      3,
		MinReplicas:          1,
		MaxReplicas:          10,
		LoadBalanceThreshold: 0.25,
		HashFunc: func(data []byte) uint32 {
			i, _ := strconv.Atoi(string(data))
			return uint32(i)
		},
	}))

	if got := m.Get("1"); got != "" {
		t.Fatalf("empty ring returned %q", got)
	}
	if err := m.Add(); err == nil {
		t.Fatal("Add without nodes should fail")
	}
	if err := m.Add("6", "4", "2", ""); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	for _, key := range []string{"2", "11", "23", "27"} {
		if got := m.Get(key); got == "" {
			t.Fatalf("Get(%q) returned an empty node", key)
		}
	}
	stats := m.GetStats()
	if len(stats) == 0 {
		t.Fatal("expected stats after Get calls")
	}
	if err := m.Remove("4"); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if err := m.Remove("missing"); err == nil {
		t.Fatal("Remove missing node should fail")
	}
	if err := m.Remove(""); err == nil {
		t.Fatal("Remove empty node should fail")
	}
}

func TestMapConcurrentAccess(t *testing.T) {
	m := NewConsistentHash(WithConsistentHashConfig(&ConHashConfig{
		DefaultReplicas: 5,
		HashFunc:        DefaultConHashConfig.HashFunc,
	}))
	if err := m.Add("a", "b", "c"); err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	// Adding an existing node must be a no-op.
	if err := m.Add("a"); err != nil {
		t.Fatalf("re-Add returned error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 1200 {
			if got := m.Get("key-" + strconv.Itoa(i)); got == "" {
				t.Error("ring returned empty node")
				return
			}
		}
	}()
	for range 20 {
		if err := m.Add("d"); err != nil {
			t.Fatalf("Add returned error: %v", err)
		}
		if err := m.Remove("d"); err != nil {
			t.Fatalf("Remove returned error: %v", err)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Get did not finish")
	}

	if got := m.Get("key"); got == "" {
		t.Fatal("ring returned empty node after churn")
	}
	if stats := m.GetStats(); len(stats) == 0 {
		t.Fatal("expected stats after Get calls")
	}
}

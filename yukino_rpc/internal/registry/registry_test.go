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

package registry

import (
	"context"
	"testing"
)

func TestCopyInstances(t *testing.T) {
	r := &Registry{
		services: map[string]map[string]Instance{
			"srv": {
				"a": {Addr: "a"},
				"b": {Addr: "b"},
			},
		},
	}
	got := r.copyInstances("srv")
	if len(got) != 2 {
		t.Fatalf("instances len = %d, want 2", len(got))
	}
	got[0].Addr = "changed"
	again := r.copyInstances("srv")
	if again[0].Addr == "changed" || again[1].Addr == "changed" {
		t.Fatal("copyInstances should not expose internal storage")
	}
}

func TestDiscoverReturnsCachedCopy(t *testing.T) {
	r := &Registry{
		services: map[string]map[string]Instance{
			"srv": {"a": {Addr: "a"}},
		},
	}
	got, err := r.Discover("srv")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	got[0].Addr = "changed"
	again, err := r.Discover("srv")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if again[0].Addr != "a" {
		t.Fatalf("cached instance mutated: %v", again)
	}
}

func TestCloseWithNilClient(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Registry{ctx: ctx, cancel: cancel}
	if err := r.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("Close should cancel context")
	}
	if err := (&Registry{}).Close(); err != nil {
		t.Fatalf("Close nil registry returned error: %v", err)
	}
}

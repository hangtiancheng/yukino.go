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

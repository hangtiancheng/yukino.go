package skiplist

import (
	"slices"
	"testing"
	"time"
)

func TestIterationAllowsReentryAndPreservesSnapshot(t *testing.T) {
	for _, kind := range []string{"All", "Range", "RangeFrom", "Keys", "Values"} {
		t.Run(kind, func(t *testing.T) {
			list := New[int, int]()
			for i := 0; i < 5; i++ {
				list.Insert(i, i)
			}
			done := make(chan []int, 1)
			go func() {
				keys := []int{}
				mutate := func(k int) { keys = append(keys, k); list.Clear(); list.Set(100, 100); list.Get(100) }
				switch kind {
				case "All":
					for k := range list.All() {
						mutate(k)
					}
				case "Range":
					for k := range list.Range(0, 5) {
						mutate(k)
					}
				case "RangeFrom":
					for k := range list.RangeFrom(0) {
						mutate(k)
					}
				case "Keys":
					for k := range list.Keys() {
						mutate(k)
					}
				case "Values":
					for v := range list.Values() {
						mutate(v)
					}
				}
				done <- keys
			}()
			select {
			case keys := <-done:
				if !slices.Equal(keys, []int{0, 1, 2, 3, 4}) {
					t.Fatal(keys)
				}
			case <-time.After(time.Second):
				t.Fatal("iterator held a lock while yielding")
			}
		})
	}
}

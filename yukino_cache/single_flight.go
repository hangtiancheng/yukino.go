package yukino_cache

import (
	"fmt"
	"sync"
)

type call struct {
	wg  sync.WaitGroup
	val any
	err error
}

// Group suppresses duplicate in-flight calls for the same key.
type SingleFlightGroup struct {
	m sync.Map
}

// Do runs fn once for a key while concurrent duplicate callers wait for the same result.
// A panic inside fn is recovered and surfaced as an error to every caller.
func (g *SingleFlightGroup) Do(key string, fn func() (any, error)) (any, error) {
	c := &call{}
	c.wg.Add(1)

	actual, loaded := g.m.LoadOrStore(key, c)
	if loaded {
		existing := actual.(*call)
		existing.wg.Wait()
		return existing.val, existing.err
	}

	defer g.m.Delete(key)
	defer c.wg.Done()

	func() {
		defer func() {
			if r := recover(); r != nil {
				c.val, c.err = nil, fmt.Errorf("singleflight: panic during call: %v", r)
			}
		}()
		c.val, c.err = fn()
	}()

	return c.val, c.err
}

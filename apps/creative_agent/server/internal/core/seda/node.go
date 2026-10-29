package seda

import "sync"

// node is the engine's wrapper around an operator, holding the graph structure (downstream/upstream) and the join-state guard.
type node struct {
	op    Operator
	nexts []*node
	prior int // number of required upstreams

	mu       sync.Mutex
	arrived  int  // number of arrived upstreams
	executed bool // whether it has executed
}

// tryClaim atomically claims the execution right of this node.
// Returning true means the caller should proceed; false means another path already claimed it (or it already ran).
// This is the "single guard" pattern: whether the join node is reached via the normal join or the execLast short-circuit,
// the same executed flag is shared, guaranteeing at most one execution.
func (n *node) tryClaim() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.executed {
		return false
	}
	n.executed = true
	return true
}

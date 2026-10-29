package seda

import (
	"fmt"
	"sync"
)

// ---------- Graph description types (used at build time) ----------

// Edge declares a directed edge from the From operator to the To operator.
type Edge struct{ From, To string }

// FlowConf describes the static structure of a flow graph.
type FlowConf struct {
	Name  string
	Ops   []string // all operator names in the graph
	Edges []Edge
	Start string // root operator name
	Last  string // terminal operator name (short-circuit target)
}

// ---------- Flow (runtime instance) ----------

// Flow is one graph-execution instance. A new Flow is created per HTTP request.
type Flow struct {
	name    string
	start   *node
	last    *node
	nodes   map[string]*node
	ctx     *Context
	retCode int
	retMsg  string
}

// BuildFlow builds a new Flow instance from a FlowConf.
// Every operator is freshly instantiated through its registered factory function.
func BuildFlow(conf FlowConf) *Flow {
	f := &Flow{
		name:  conf.Name,
		nodes: make(map[string]*node),
		ctx:   NewContext(),
	}
	for _, name := range conf.Ops {
		builder, ok := registry[name]
		if !ok {
			panic(fmt.Sprintf("seda: operator %q not registered", name))
		}
		f.nodes[name] = &node{op: builder()}
	}
	for _, e := range conf.Edges {
		from := f.nodes[e.From]
		to := f.nodes[e.To]
		from.nexts = append(from.nexts, to)
		to.prior++
	}
	f.start = f.nodes[conf.Start]
	f.last = f.nodes[conf.Last]
	return f
}

// GetContext returns the flow's shared context.
// Callers use it to inject data (e.g. the HTTP body) before WaitExec.
func (f *Flow) GetContext() *Context { return f.ctx }

// WaitExec drives the flow from the start node and blocks until all goroutines converge.
// It returns the overall execution code and message.
func (f *Flow) WaitExec() (int, string) {
	f.run(f.start)
	return f.retCode, f.retMsg
}

// run drives execution forward starting from n.
func (f *Flow) run(n *node) {
	// --- Join: wait for all required upstreams ---
	n.mu.Lock()
	n.arrived++
	if n.arrived < n.prior {
		n.mu.Unlock()
		return // not all arrived yet; the last upstream triggers the actual execution
	}
	if n.executed {
		n.mu.Unlock()
		return // already claimed (e.g. execLast has run)
	}
	n.executed = true
	n.mu.Unlock()

	// --- Execute ---
	code, msg := f.safeDo(n)

	// --- Short-circuit ---
	if code < 0 {
		f.retCode = code
		f.retMsg = msg
		f.execLast()
		return
	}

	// --- Forward drive: fork or chain ---
	if len(n.nexts) == 1 {
		f.run(n.nexts[0])
	} else if len(n.nexts) > 1 {
		var wg sync.WaitGroup
		for _, nx := range n.nexts {
			wg.Add(1)
			go func(target *node) {
				defer wg.Done()
				f.run(target)
			}(nx)
		}
		wg.Wait()
	}
	// No downstream: leaf node, nothing to forward
}

// safeDo executes the operator's DoImpl with panic protection.
func (f *Flow) safeDo(n *node) (code int, msg string) {
	defer func() {
		if r := recover(); r != nil {
			code = -100
			msg = fmt.Sprintf("seda: operator %q panicked: %v", n.op.Name(), r)
		}
	}()
	return n.op.DoImpl(f.ctx)
}

// execLast unconditionally executes the last operator (if not already executed).
// It reuses tryClaim, sharing the same executed guard as the normal join path,
// guaranteeing the last operator runs at most once regardless of the path taken.
func (f *Flow) execLast() {
	if !f.last.tryClaim() {
		return
	}
	f.safeDo(f.last)
}

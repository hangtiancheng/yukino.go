package seda_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/core/seda"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"
)

// ---------- Test operators ----------

type testOp struct {
	name   string
	doFunc func(ctx *seda.Context) (int, string)
}

func (t *testOp) Name() string                           { return t.name }
func (t *testOp) DoImpl(ctx *seda.Context) (int, string) { return t.doFunc(ctx) }

func newTestOp(name string, fn func(ctx *seda.Context) (int, string)) seda.OperatorBuilder {
	return func() seda.Operator {
		return &testOp{name: name, doFunc: fn}
	}
}

// ---------- Tests ----------

// TestFlow_LinearChain tests a simple A → B → C chain.
func TestFlow_LinearChain(t *testing.T) {
	var mu sync.Mutex
	var order []string

	seda.Register("A", newTestOp("A", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "A")
		mu.Unlock()
		ctx.Set("val", 42)
		return 0, ""
	}))
	seda.Register("B", newTestOp("B", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "B")
		mu.Unlock()
		v := ctx.Get("val")
		testx.Equal(t, 42, v)
		return 0, ""
	}))
	seda.Register("C", newTestOp("C", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "C")
		mu.Unlock()
		return 0, ""
	}))

	conf := seda.FlowConf{
		Name:  "linear",
		Ops:   []string{"A", "B", "C"},
		Start: "A",
		Last:  "C",
		Edges: []seda.Edge{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
		},
	}

	flow := seda.BuildFlow(conf)
	code, msg := flow.WaitExec()
	testx.Equal(t, 0, code)
	testx.Empty(t, msg)
	testx.Equal(t, []string{"A", "B", "C"}, order)
}

// TestFlow_ForkJoin tests a fork pattern: root → a, root → b; both → end.
func TestFlow_ForkJoin(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var endCount int

	seda.Register("root", newTestOp("root", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "root")
		mu.Unlock()
		ctx.Set("data", "shared")
		return 0, ""
	}))
	seda.Register("A", newTestOp("A", func(ctx *seda.Context) (int, string) {
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		order = append(order, "A")
		mu.Unlock()
		testx.Equal(t, "shared", ctx.Get("data"))
		return 0, ""
	}))
	seda.Register("B", newTestOp("B", func(ctx *seda.Context) (int, string) {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		order = append(order, "B")
		mu.Unlock()
		testx.Equal(t, "shared", ctx.Get("data"))
		return 0, ""
	}))
	seda.Register("end", newTestOp("end", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "end")
		endCount++
		mu.Unlock()
		return 0, ""
	}))

	conf := seda.FlowConf{
		Name:  "forkjoin",
		Ops:   []string{"root", "A", "B", "end"},
		Start: "root",
		Last:  "end",
		Edges: []seda.Edge{
			{From: "root", To: "A"},
			{From: "root", To: "B"},
			{From: "A", To: "end"},
			{From: "B", To: "end"},
		},
	}

	flow := seda.BuildFlow(conf)
	code, msg := flow.WaitExec()
	testx.Equal(t, 0, code)
	testx.Empty(t, msg)
	// root must be first, end must be last
	testx.Equal(t, "root", order[0])
	testx.Contains(t, order[1:3], "A")
	testx.Contains(t, order[1:3], "B")
	testx.Equal(t, "end", order[3])
	testx.Equal(t, 4, len(order))
	// end must execute exactly once
	testx.Equal(t, 1, endCount, "join node must execute exactly once")
}

// TestFlow_ShortCircuit tests that code < 0 skips to last operator.
func TestFlow_ShortCircuit(t *testing.T) {
	var mu sync.Mutex
	var order []string

	seda.Register("start", newTestOp("start", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "start")
		mu.Unlock()
		return -1, "error occurred"
	}))
	seda.Register("middle", newTestOp("middle", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "middle")
		mu.Unlock()
		return 0, ""
	}))
	seda.Register("last", newTestOp("last", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		order = append(order, "last")
		mu.Unlock()
		return 0, ""
	}))

	conf := seda.FlowConf{
		Name:  "shortcircuit",
		Ops:   []string{"start", "middle", "last"},
		Start: "start",
		Last:  "last",
		Edges: []seda.Edge{
			{From: "start", To: "middle"},
			{From: "middle", To: "last"},
		},
	}

	flow := seda.BuildFlow(conf)
	code, msg := flow.WaitExec()
	testx.Equal(t, -1, code)
	testx.Equal(t, "error occurred", msg)
	testx.Equal(t, []string{"start", "last"}, order)
}

// TestFlow_ShortCircuitOnFork ensures short-circuit doesn't double-execute last op
// when forked paths also converge on it.
func TestFlow_ShortCircuitOnFork(t *testing.T) {
	var mu sync.Mutex
	var lastCount int

	seda.Register("rootF", newTestOp("rootF", func(ctx *seda.Context) (int, string) {
		return 0, ""
	}))
	seda.Register("A_s", newTestOp("A_s", func(ctx *seda.Context) (int, string) {
		time.Sleep(30 * time.Millisecond)
		return 0, ""
	}))
	seda.Register("B_s", newTestOp("B_s", func(ctx *seda.Context) (int, string) {
		return -1, "short circuit!"
	}))
	seda.Register("end_s", newTestOp("end_s", func(ctx *seda.Context) (int, string) {
		mu.Lock()
		lastCount++
		mu.Unlock()
		return 0, ""
	}))

	conf := seda.FlowConf{
		Name:  "shortfork",
		Ops:   []string{"rootF", "A_s", "B_s", "end_s"},
		Start: "rootF",
		Last:  "end_s",
		Edges: []seda.Edge{
			{From: "rootF", To: "A_s"},
			{From: "rootF", To: "B_s"},
			{From: "A_s", To: "end_s"},
			{From: "B_s", To: "end_s"},
		},
	}

	flow := seda.BuildFlow(conf)
	flow.WaitExec()
	testx.Equal(t, 1, lastCount, "last op must execute exactly once with tryClaim guard")
}

// TestFlow_SliceStreaming tests the full producer-consumer streaming pattern
// within a flow (simulating supervisor → con_writer).
func TestFlow_SliceStreaming(t *testing.T) {
	const sseKey = "sse_frames"
	var received []string
	var mu sync.Mutex

	seda.Register("producer", newTestOp("producer", func(ctx *seda.Context) (int, string) {
		for i := 1; i <= 5; i++ {
			time.Sleep(5 * time.Millisecond)
			ctx.Push(sseKey, fmt.Sprintf("frame_%d", i))
		}
		ctx.Push(sseKey, "__END__")
		return 0, ""
	}))
	seda.Register("consumer", newTestOp("consumer", func(ctx *seda.Context) (int, string) {
		slice := ctx.GetSliceWithInit(sseKey)
		cursor := 0
		for {
			ch := slice.WatchPush()
			diff, next := slice.DiffFrom(cursor)
			cursor = next
			for _, v := range diff {
				s := v.(string)
				mu.Lock()
				received = append(received, s)
				mu.Unlock()
				if s == "__END__" {
					return 0, ""
				}
			}
			<-ch
		}
	}))
	seda.Register("theend", newTestOp("theend", func(ctx *seda.Context) (int, string) {
		return 0, ""
	}))

	conf := seda.FlowConf{
		Name:  "stream",
		Ops:   []string{"producer", "consumer", "theend"},
		Start: "producer",
		Last:  "theend",
		Edges: []seda.Edge{
			{From: "producer", To: "consumer"},
			{From: "consumer", To: "theend"},
		},
	}

	flow := seda.BuildFlow(conf)
	code, msg := flow.WaitExec()
	testx.Equal(t, 0, code)
	testx.Empty(t, msg)
	testx.Equal(t, []string{"frame_1", "frame_2", "frame_3", "frame_4", "frame_5", "__END__"}, received)
}

// ---------- Context 单值 & 流式 ----------

// TestContext_SetGet_Value tests single-value Set/Get.
func TestContext_SetGet_Value(t *testing.T) {
	ctx := seda.NewContext()
	ctx.Set("key1", "value1")
	testx.Equal(t, "value1", ctx.Get("key1"))
	testx.Nil(t, ctx.Get("key2"))
}

// TestContext_Wait_BlocksUntilSet tests that Wait blocks until Set is called.
func TestContext_Wait_BlocksUntilSet(t *testing.T) {
	ctx := seda.NewContext()

	var wg sync.WaitGroup
	wg.Add(1)
	var result any
	go func() {
		defer wg.Done()
		result = ctx.Wait("delayed_key")
	}()

	time.Sleep(50 * time.Millisecond)
	ctx.Set("delayed_key", "hello")

	wg.Wait()
	testx.Equal(t, "hello", result)
}

// TestContext_Wait_ReturnsImmediately_WhenAlreadySet tests Wait returns without blocking
// when the key has been set before Wait is called.
func TestContext_Wait_ReturnsImmediately_WhenAlreadySet(t *testing.T) {
	ctx := seda.NewContext()
	ctx.Set("early_key", "ready")
	result := ctx.Wait("early_key")
	testx.Equal(t, "ready", result)
}

// TestContext_Push_WatchPush_DiffFrom tests the Slice streaming pattern.
func TestContext_Push_WatchPush_DiffFrom(t *testing.T) {
	ctx := seda.NewContext()

	slice := ctx.GetSliceWithInit("sse")

	cursor := 0
	ch := slice.WatchPush()
	diff, next := slice.DiffFrom(cursor)
	cursor = next
	testx.Empty(t, diff)

	ctx.Push("sse", "frame1")
	ctx.Push("sse", "frame2")

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("WatchPush timed out")
	}

	diff, cursor = slice.DiffFrom(cursor)
	testx.Equal(t, []any{"frame1", "frame2"}, diff)
	testx.Equal(t, 2, cursor)
}

// TestContext_Push_MultipleConsumers tests that multiple consumers can independently track.
func TestContext_Push_MultipleConsumers(t *testing.T) {
	ctx := seda.NewContext()

	s1 := ctx.GetSliceWithInit("key")
	s2 := ctx.GetSliceWithInit("key")

	c1, c2 := 0, 0
	ch1 := s1.WatchPush()
	ch2 := s2.WatchPush()

	ctx.Push("key", "a")

	<-ch1
	<-ch2

	d1, n1 := s1.DiffFrom(c1)
	d2, n2 := s2.DiffFrom(c2)
	testx.Equal(t, []any{"a"}, d1)
	testx.Equal(t, []any{"a"}, d2)
	testx.Equal(t, 1, n1)
	testx.Equal(t, 1, n2)
}

func TestContext_Set_MultipleWaiters(t *testing.T) {
	ctx := seda.NewContext()

	var wg sync.WaitGroup
	results := make([]any, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = ctx.Wait("broadcast")
		}(i)
	}
	time.Sleep(30 * time.Millisecond)
	ctx.Set("broadcast", "all")

	wg.Wait()
	for i, r := range results {
		testx.Equal(t, "all", r, "consumer %d should get same value", i)
	}
}

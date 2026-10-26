package seda

import (
	"fmt"
	"sync"
)

// ---------- 图描述类型（构建期使用） ----------

// Edge 声明从 From 算子到 To 算子的有向边。
type Edge struct{ From, To string }

// FlowConf 描述一张 flow 图的静态结构。
type FlowConf struct {
	Name  string
	Ops   []string // 图中所有算子名
	Edges []Edge
	Start string // 根算子名
	Last  string // 终结算子名（短路目标）
}

// ---------- Flow（运行时实例） ----------

// Flow 是一次图执行实例。每个 HTTP 请求创建一个新的 Flow。
type Flow struct {
	name    string
	start   *node
	last    *node
	nodes   map[string]*node
	ctx     *Context
	retCode int
	retMsg  string
}

// BuildFlow 按 FlowConf 构建一个新的 Flow 实例。
// 每个算子都会通过其注册的工厂函数全新实例化。
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

// GetContext 返回 flow 的共享上下文。
// 供调用方在 WaitExec 之前注入数据（如 HTTP body）。
func (f *Flow) GetContext() *Context { return f.ctx }

// WaitExec 从 start 节点开始驱动 flow，阻塞直到所有 goroutine 收敛。
// 返回整体执行码和消息。
func (f *Flow) WaitExec() (int, string) {
	f.run(f.start)
	return f.retCode, f.retMsg
}

// run 从 n 开始正向驱动执行。
func (f *Flow) run(n *node) {
	// --- 汇合：等齐所有必需上游 ---
	n.mu.Lock()
	n.arrived++
	if n.arrived < n.prior {
		n.mu.Unlock()
		return // 还没等齐；最后一个上游会触发真正执行
	}
	if n.executed {
		n.mu.Unlock()
		return // 已被抢占（如 execLast 已执行）
	}
	n.executed = true
	n.mu.Unlock()

	// --- 执行 ---
	code, msg := f.safeDo(n)

	// --- 短路 ---
	if code < 0 {
		f.retCode = code
		f.retMsg = msg
		f.execLast()
		return
	}

	// --- 正向驱动：fork 或链式 ---
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
	// 无下游：叶子节点，不需要转发
}

// safeDo 带 panic 兜底地执行算子的 DoImpl。
func (f *Flow) safeDo(n *node) (code int, msg string) {
	defer func() {
		if r := recover(); r != nil {
			code = -100
			msg = fmt.Sprintf("seda: operator %q panicked: %v", n.op.Name(), r)
		}
	}()
	return n.op.DoImpl(f.ctx)
}

// execLast 无条件执行最后一个算子（若尚未执行）。
// 复用 tryClaim，与正常 join 路径共享同一个 executed 守卫，
// 保证无论从哪条路径到达，最后算子至多执行一次。
func (f *Flow) execLast() {
	if !f.last.tryClaim() {
		return
	}
	f.safeDo(f.last)
}

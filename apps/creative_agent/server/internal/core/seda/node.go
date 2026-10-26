package seda

import "sync"

// node 是引擎对算子的包装，持有图结构（下游/上游）和汇合状态守卫。
type node struct {
	op    Operator
	nexts []*node
	prior int // 必需上游数量

	mu       sync.Mutex
	arrived  int  // 已到达的上游数
	executed bool // 是否已执行
}

// tryClaim 原子抢占本节点的执行权。
// 返回 true 表示调用方应继续执行；false 表示已被其他路径抢占（或已执行过）。
// 这是"单一守卫"模式：无论从正常 join 还是 execLast 短路到达汇合节点，
// 都共享同一个 executed 标志，保证至多执行一次。
func (n *node) tryClaim() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.executed {
		return false
	}
	n.executed = true
	return true
}

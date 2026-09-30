package load_balance

import (
	"sync/atomic"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/registry"
)

type RoundRobin struct {
	idx atomic.Uint64
}

func NewRR() *RoundRobin {
	r := &RoundRobin{}
	return r
}

func (r *RoundRobin) Select(list []registry.Instance) registry.Instance {
	if len(list) == 0 {
		return registry.Instance{}
	}
	i := r.idx.Add(1)
	return list[(i-1)%uint64(len(list))]
}

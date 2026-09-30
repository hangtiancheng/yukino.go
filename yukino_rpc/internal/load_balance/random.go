package load_balance

import (
	"math/rand"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/registry"
)

type Random struct {
	r *rand.Rand
	m sync.Mutex
}

func NewRandom() *Random {
	return &Random{
		r: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (r *Random) Select(list []registry.Instance) registry.Instance {
	if len(list) == 0 {
		return registry.Instance{}
	}

	r.m.Lock()
	defer r.m.Unlock()
	return list[r.r.Intn(len(list))]
}

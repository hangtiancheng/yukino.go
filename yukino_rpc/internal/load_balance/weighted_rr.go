package load_balance

import (
	"sync"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/registry"
)

// WeightedRR implements smooth weighted round-robin selection.
type WeightedRR struct {
	mu            sync.Mutex
	weights       []int
	currentWeight []int
	totalWeight   int
}

func NewWeightedRR(weights []int) *WeightedRR {
	w := &WeightedRR{
		weights:       make([]int, len(weights)),
		currentWeight: make([]int, len(weights)),
	}

	total := 0
	for i, wt := range weights {
		if wt < 0 {
			wt = 0
		}
		w.weights[i] = wt
		total += wt
	}
	w.totalWeight = total

	return w
}

func (w *WeightedRR) Select(list []registry.Instance) registry.Instance {
	if len(list) == 0 {
		return registry.Instance{}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(list) != len(w.weights) {
		return registry.Instance{}
	}
	if w.totalWeight <= 0 {
		return registry.Instance{}
	}

	maxIdx := -1
	for i := range list {
		w.currentWeight[i] += w.weights[i]

		if maxIdx == -1 || w.currentWeight[i] > w.currentWeight[maxIdx] {
			maxIdx = i
		}
	}

	w.currentWeight[maxIdx] -= w.totalWeight
	return list[maxIdx]
}

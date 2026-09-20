package tcc_demo

import (
	"context"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Runs many concurrent transactions while the monitor goroutine polls hanging
// transactions at a high frequency. This overlaps the monitor's reads of
// transaction records with the try phase's concurrent status updates, which is
// where unsynchronized shared state would be reported by `go test -race`.
func Test_txmanager_transaction_concurrent_with_monitor(t *testing.T) {
	txmanager := NewTXManager(newMockTXStore(), WithMonitorTick(5*time.Millisecond))
	defer txmanager.Stop()

	// Register 10 components
	componentsCnt := 10
	for i := range componentsCnt {
		componentID := strconv.Itoa(i)
		if err := txmanager.Register(newMockComponent(componentID)); err != nil {
			t.Error(err)
			return
		}
	}

	// 50 concurrent transactions, each randomly picking 3 components
	ctx := context.Background()
	concurrentTXs := 50
	componentReqCnt := 3
	var wg sync.WaitGroup
	for range concurrentTXs {
		wg.Go(func() {
			randInst := rand.New(rand.NewSource(time.Now().UnixNano()))
			componentSet := make(map[string]struct{}, componentReqCnt)
			for len(componentSet) < componentReqCnt {
				componentSet[strconv.Itoa(randInst.Intn(componentsCnt))] = struct{}{}
			}

			componentReqs := make([]*RequestEntity, 0, componentReqCnt)
			for componentID := range componentSet {
				componentReqs = append(componentReqs, &RequestEntity{
					ComponentID: componentID,
				})
			}

			txid, ok, err := txmanager.Transaction(ctx, componentReqs...)
			if err != nil {
				t.Error(err)
				return
			}
			if !ok {
				t.Error("expected true, got false")
				return
			}
			tx, err := txmanager.txStore.GetTX(ctx, txid)
			if err != nil {
				t.Error(err)
				return
			}
			if tx.Status != TXSuccessful {
				t.Errorf("expected %s, got %s", TXSuccessful, tx.Status)
			}
		})
	}

	wg.Wait()
}

// One component rejects immediately while another one is still slow in the try
// phase. The transaction must end up failed and the component whose try
// succeeded must receive the second-phase cancel.
func Test_txmanager_transaction_mixed_slow_try(t *testing.T) {
	txmanager := NewTXManager(newMockTXStore(), WithMonitorTick(time.Second))
	defer txmanager.Stop()

	fast := &mockComponent{id: "fast", statusMachine: make(map[string]Status)}
	slow := &mockComponent{id: "slow", statusMachine: make(map[string]Status)}
	if err := txmanager.Register(fast); err != nil {
		t.Error(err)
		return
	}
	if err := txmanager.Register(slow); err != nil {
		t.Error(err)
		return
	}

	ctx := context.Background()
	txid, ok, err := txmanager.Transaction(ctx,
		&RequestEntity{ComponentID: "fast"},
		&RequestEntity{ComponentID: "slow", Request: map[string]any{
			"hanging_flag": true,
		}},
	)
	if err != nil {
		t.Error(err)
		return
	}
	if ok {
		t.Error("expected false, got true")
	}
	tx, err := txmanager.txStore.GetTX(ctx, txid)
	if err != nil {
		t.Error(err)
		return
	}
	if tx.Status != TXFailure {
		t.Errorf("expected %s, got %s", TXFailure, tx.Status)
	}
	if got := fast.statusMachine[txid]; got != StatusCanceled {
		t.Errorf("expected %s, got %s", StatusCanceled, got)
	}
}

func Test_txmanager_transaction_invalid_args(t *testing.T) {
	txmanager := NewTXManager(newMockTXStore())
	defer txmanager.Stop()

	ctx := context.Background()
	// Empty request list
	if _, _, err := txmanager.Transaction(ctx); err == nil {
		t.Error("expected error for empty reqs, got nil")
	}

	if err := txmanager.Register(newMockComponent("dup")); err != nil {
		t.Error(err)
		return
	}
	// Repeated component id
	if _, _, err := txmanager.Transaction(ctx,
		&RequestEntity{ComponentID: "dup"},
		&RequestEntity{ComponentID: "dup"},
	); err == nil {
		t.Error("expected error for repeated component, got nil")
	}
	// Unregistered component id
	if _, _, err := txmanager.Transaction(ctx,
		&RequestEntity{ComponentID: "missing"},
	); err == nil {
		t.Error("expected error for missing component, got nil")
	}
}

// Registers and looks up components concurrently; duplicates must be rejected
// and every registered id must resolve to exactly one component.
func Test_registryCenter_concurrent_register_and_get(t *testing.T) {
	rc := newRegistryCenter()

	const (
		goroutines = 100
		components = 10
	)
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			componentID := strconv.Itoa(i % components)
			// Duplicate registrations may fail, which is expected
			_ = rc.register(newMockComponent(componentID))
			_, _ = rc.getComponents(componentID)
		})
	}
	wg.Wait()

	for i := range components {
		componentsByID, err := rc.getComponents(strconv.Itoa(i))
		if err != nil {
			t.Error(err)
			return
		}
		if len(componentsByID) != 1 {
			t.Errorf("expected 1 component for id %d, got %d", i, len(componentsByID))
		}
	}
	if _, err := rc.getComponents("missing"); err == nil {
		t.Error("expected error for missing component, got nil")
	}
}

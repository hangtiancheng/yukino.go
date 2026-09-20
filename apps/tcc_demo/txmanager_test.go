// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package tcc_demo

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type mockTXStore struct {
	mutex sync.Mutex
	txs   map[string]*Transaction
}

func newMockTXStore() TXStore {
	return &mockTXStore{
		txs: make(map[string]*Transaction),
	}
}

// Creates a transaction detail record
func (m *mockTXStore) CreateTX(ctx context.Context, components ...TCCComponent) (string, error) {
	txid := uuid.NewString()
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.txs[txid]; ok {
		return "", fmt.Errorf("repeat txid: %s", txid)
	}

	componentTryEntities := make([]*ComponentTryEntity, 0, len(components))
	for _, component := range components {
		componentTryEntities = append(componentTryEntities, &ComponentTryEntity{
			ComponentID: component.ID(),
			TryStatus:   TryHanging,
		})
	}

	m.txs[txid] = &Transaction{
		TXID:       txid,
		Status:     TXHanging,
		CreatedAt:  time.Now(),
		Components: componentTryEntities,
	}

	return txid, nil
}

// Updates transaction progress: updates each component's try response result
func (m *mockTXStore) TXUpdate(ctx context.Context, txID string, componentID string, accept bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	tx, ok := m.txs[txID]
	if !ok {
		return fmt.Errorf("[TXUpdate]invalid txid: %s", txID)
	}
	for _, component := range tx.Components {
		if component.ComponentID != componentID {
			continue
		}
		if component.TryStatus != TryHanging {
			return fmt.Errorf("invalid component status: %s, componentId: %s, txId: %s", component.TryStatus, componentID, txID)
		}
		if accept {
			component.TryStatus = TrySuccessful
		} else {
			component.TryStatus = TryFailure
		}
		return nil
	}
	return fmt.Errorf("[TXUpdate]invalid component id: %s for txid: %s", componentID, txID)
}

// Submits the final transaction status, indicating success or failure
func (m *mockTXStore) TXSubmit(ctx context.Context, txID string, success bool) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	tx, ok := m.txs[txID]
	if !ok {
		return fmt.Errorf("[TXSubmit]invalid txid: %s", txID)
	}
	if success {
		if tx.Status != TXHanging && tx.Status != TXSuccessful {
			return fmt.Errorf("invalid txstatus: %s, txid: %s", tx.Status, txID)
		}
		tx.Status = TXSuccessful
	} else {
		if tx.Status != TXHanging && tx.Status != TXFailure {
			return fmt.Errorf("invalid txstatus: %s, txid: %s", tx.Status, txID)
		}
		tx.Status = TXFailure
	}
	return nil
}

// copyTransaction returns a deep copy so that readers outside the store lock
// never share mutable state with concurrent TXUpdate writers.
func copyTransaction(tx *Transaction) *Transaction {
	components := make([]*ComponentTryEntity, 0, len(tx.Components))
	for _, component := range tx.Components {
		components = append(components, &ComponentTryEntity{
			ComponentID: component.ComponentID,
			TryStatus:   component.TryStatus,
		})
	}
	return &Transaction{
		TXID:       tx.TXID,
		Status:     tx.Status,
		CreatedAt:  tx.CreatedAt,
		Components: components,
	}
}

// Retrieves all incomplete transactions
func (m *mockTXStore) GetHangingTXs(ctx context.Context) ([]*Transaction, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	var hangingTXs []*Transaction
	for _, tx := range m.txs {
		if tx.Status != TXHanging {
			continue
		}
		hangingTXs = append(hangingTXs, copyTransaction(tx))
	}
	return hangingTXs, nil
}

// Retrieves a specific transaction by ID
func (m *mockTXStore) GetTX(ctx context.Context, txID string) (*Transaction, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	tx, ok := m.txs[txID]
	if !ok {
		return nil, fmt.Errorf("[GetTX]invalid txid: %s", txID)
	}
	return copyTransaction(tx), nil
}

// Locks the entire TXStore module
func (m *mockTXStore) Lock(ctx context.Context, expireDuration time.Duration) error {
	return nil
}

// Unlocks the TXStore module
func (m *mockTXStore) Unlock(ctx context.Context) error {
	return nil
}

type Status string

const (
	StatusTried     = "tried"
	StatusConfirmed = "confirmed"
	StatusCanceled  = "canceled"
)

type mockComponent struct {
	id            string
	mutex         sync.Mutex
	statusMachine map[string]Status
}

func newMockComponent(id string) TCCComponent {
	return &mockComponent{
		id:            id,
		statusMachine: make(map[string]Status),
	}
}

// Returns the unique component ID
func (m *mockComponent) ID() string {
	return m.id
}

// Executes the first-phase try operation
func (m *mockComponent) Try(ctx context.Context, req *TCCReq) (*TCCResp, error) {
	resp := TCCResp{
		ComponentID: m.id,
		TXID:        req.TXID,
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.statusMachine[req.TXID] == StatusCanceled {
		return &resp, nil
	}

	if req.Data["reject_flag"] == true {
		m.statusMachine[req.TXID] = StatusCanceled
		return &resp, nil
	}

	if req.Data["hanging_flag"] == true {
		<-time.After(time.Second)
		return &resp, nil
	}

	if m.statusMachine[req.TXID] != StatusConfirmed {
		m.statusMachine[req.TXID] = StatusTried
	}

	resp.ACK = true
	return &resp, nil
}

// Executes the second-phase confirm operation
func (m *mockComponent) Confirm(ctx context.Context, txID string) (*TCCResp, error) {
	resp := TCCResp{
		ComponentID: m.id,
		TXID:        txID,
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.statusMachine[txID] != StatusTried && m.statusMachine[txID] != StatusConfirmed {
		return &resp, nil
	}

	resp.ACK = true
	m.statusMachine[txID] = StatusConfirmed
	return &resp, nil
}

// Executes the second-phase cancel operation
func (m *mockComponent) Cancel(ctx context.Context, txID string) (*TCCResp, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.statusMachine[txID] == StatusConfirmed {
		return nil, errors.New("invalid status machine: [confirmed] when canceling")
	}

	m.statusMachine[txID] = StatusCanceled
	return &TCCResp{
		ComponentID: m.id,
		ACK:         true,
		TXID:        txID,
	}, nil
}

func Test_txmanager_transaction_success(t *testing.T) {
	txmanager := NewTXManager(newMockTXStore())
	defer txmanager.Stop()

	// Register 5 components
	componentsCnt := 5
	componentReqs := make([]*RequestEntity, 0, componentsCnt)
	ctx := context.Background()
	for i := range componentsCnt {
		componentID := strconv.Itoa(i)
		if err := txmanager.Register(newMockComponent(componentID)); err != nil {
			t.Error(err)
			return
		}
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
	}
	tx, err := txmanager.txStore.GetTX(ctx, txid)
	if err != nil {
		t.Error(err)
		return
	}
	if tx.Status != TXSuccessful {
		t.Errorf("expected %s, got %s", TXSuccessful, tx.Status)
	}
}

// Verify distributed transaction failure scenario
func Test_txmanager_transaction_fail(t *testing.T) {
	txmanager := NewTXManager(newMockTXStore())
	defer txmanager.Stop()

	// Register 5 components
	componentsCnt := 5
	componentReqs := make([]*RequestEntity, 0, componentsCnt)
	ctx := context.Background()
	for i := range componentsCnt {
		componentID := strconv.Itoa(i)
		if err := txmanager.Register(newMockComponent(componentID)); err != nil {
			t.Error(err)
			return
		}
		componentReqs = append(componentReqs, &RequestEntity{
			ComponentID: componentID,
			Request: map[string]any{
				"reject_flag": true,
			},
		})
	}

	txid, ok, err := txmanager.Transaction(ctx, componentReqs...)
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
}

func Test_txmanager_transaction_concurrent(t *testing.T) {
	txmanager := NewTXManager(newMockTXStore(), WithMonitorTick(0), WithTimeout(0))
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

	// 100 concurrent distributed transactions, randomly picking 3 components
	ctx := context.Background()
	concurrentTXs := 100
	componentReqCnt := 3
	var wg sync.WaitGroup
	for range concurrentTXs {
		wg.Go(func() {
			randInst := rand.New(rand.NewSource(time.Now().UnixNano()))
			componentSet := make(map[string]struct{}, componentReqCnt)
			for len(componentSet) < componentReqCnt {
				componentID := strconv.Itoa(randInst.Intn(componentsCnt))
				componentSet[componentID] = struct{}{}
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

func Test_txmanager_transaction_advance_progress(t *testing.T) {
	txmanager := NewTXManager(newMockTXStore(), WithMonitorTick(100*time.Millisecond))
	defer txmanager.Stop()

	// Register 5 components
	componentsCnt := 5
	componentReqs := make([]*RequestEntity, 0, componentsCnt)
	ctx := context.Background()
	for i := range componentsCnt {
		componentID := strconv.Itoa(i)
		if err := txmanager.Register(newMockComponent(componentID)); err != nil {
			t.Error(err)
			return
		}
		componentReqs = append(componentReqs, &RequestEntity{
			ComponentID: componentID,
			Request: map[string]any{
				"hanging_flag": true,
			},
		})
	}

	txid, ok, err := txmanager.Transaction(ctx, componentReqs...)
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
}

func Test_txManager_backOffTick(t *testing.T) {
	txManager := NewTXManager(newMockTXStore(), WithMonitorTick(time.Second))
	defer txManager.stop()
	got := txManager.backOffTick(time.Second)
	if got != 2*time.Second {
		t.Errorf("expected %v, got %v", 2*time.Second, got)
	}
	got = txManager.backOffTick(got)
	if got != 4*time.Second {
		t.Errorf("expected %v, got %v", 4*time.Second, got)
	}
	got = txManager.backOffTick(got)
	if got != 8*time.Second {
		t.Errorf("expected %v, got %v", 8*time.Second, got)
	}
	got = txManager.backOffTick(got)
	if got != 8*time.Second {
		t.Errorf("expected %v, got %v", 8*time.Second, got)
	}
}

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
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/tcc_demo/log"
)

// 1. Transaction log storage module
// 2. TCC component registration module
// 3. Connects the two workflows
type TXManager struct {
	ctx            context.Context
	stop           context.CancelFunc
	opts           *Options
	txStore        TXStore
	registryCenter *registryCenter
}

func NewTXManager(txStore TXStore, opts ...Option) *TXManager {
	ctx, cancel := context.WithCancel(context.Background())
	txManager := TXManager{
		opts:           &Options{},
		txStore:        txStore,
		registryCenter: newRegistryCenter(),
		ctx:            ctx,
		stop:           cancel,
	}

	for _, opt := range opts {
		opt(txManager.opts)
	}

	repair(txManager.opts)

	go txManager.run()
	return &txManager
}

func (t *TXManager) Stop() {
	t.stop()
}

func (t *TXManager) Register(component TCCComponent) error {
	return t.registryCenter.register(component)
}

// Transaction
func (t *TXManager) Transaction(ctx context.Context, reqs ...*RequestEntity) (string, bool, error) {
	ctx2, cancel := context.WithTimeout(ctx, t.opts.Timeout)
	defer cancel()

	// Retrieve all components
	componentEntities, err := t.getComponents(ctx2, reqs...)
	if err != nil {
		return "", false, err
	}

	// 1. Create a transaction detail record and obtain a globally unique transaction ID
	txID, err := t.txStore.CreateTX(ctx2, componentEntities.ToComponents()...)
	if err != nil {
		return "", false, err
	}

	// 2. Two-phase commit: try-confirm/cancel
	return txID, t.twoPhaseCommit(ctx2, txID, componentEntities), nil
}

func (t *TXManager) backOffTick(tick time.Duration) time.Duration {
	tick <<= 1
	if threshold := t.opts.MonitorTick << 3; tick > threshold {
		return threshold
	}
	return tick
}

func (t *TXManager) run() {
	var tick time.Duration
	var err error
	for {
		// On failure, apply backoff by increasing the tick interval
		if err == nil {
			tick = t.opts.MonitorTick
		} else {
			tick = t.backOffTick(tick)
		}
		select {
		case <-t.ctx.Done():
			return

		case <-time.After(tick):
			// Acquire lock to prevent duplicate monitoring across distributed nodes
			if err = t.txStore.Lock(t.ctx, t.opts.MonitorTick); err != nil {
				// If lock acquisition fails (likely held by another node), do not increase backoff
				err = nil
				continue
			}

			// Retrieve transactions still in hanging state
			var txs []*Transaction
			if txs, err = t.txStore.GetHangingTXs(t.ctx); err != nil {
				_ = t.txStore.Unlock(t.ctx)
				continue
			}

			err = t.batchAdvanceProgress(txs)
			_ = t.txStore.Unlock(t.ctx)
		}
	}
}

func (t *TXManager) batchAdvanceProgress(txs []*Transaction) error {
	// Advance progress for each transaction
	errCh := make(chan error)
	go func() {
		// Advance progress for each transaction concurrently
		var wg sync.WaitGroup
		for _, tx := range txs {
			// shadow
			wg.Go(func() {
				// Each goroutine handles one transaction
				if err := t.advanceProgress(tx); err != nil {
					// Send errors to errCh
					errCh <- err
				}
			})
		}
		wg.Wait()
		close(errCh)
	}()

	var firstErr error
	// Block here until all goroutines complete and the channel is closed
	for err := range errCh {
		// Record the first error encountered
		if firstErr != nil {
			continue
		}
		firstErr = err
	}

	return firstErr
}

// Advances progress for a given transaction ID
func (t *TXManager) advanceProgressByTXID(txID string) error {
	// Retrieve the transaction log record
	tx, err := t.txStore.GetTX(t.ctx, txID)
	if err != nil {
		return err
	}
	return t.advanceProgress(tx)
}

// Advances progress for a given transaction
func (t *TXManager) advanceProgress(tx *Transaction) error {
	// Infer the transaction status from each component's try result
	txStatus := tx.getStatus(time.Now().Add(-t.opts.Timeout))
	// Skip transactions still in hanging state
	if txStatus == TXHanging {
		return nil
	}

	// Configure different handlers based on transaction success or failure
	success := txStatus == TXSuccessful
	var confirmOrCancel func(ctx context.Context, component TCCComponent) (*TCCResp, error)
	var txAdvanceProgress func(ctx context.Context) error
	if success {
		confirmOrCancel = func(ctx context.Context, component TCCComponent) (*TCCResp, error) {
			// Execute second-phase confirm on the component
			return component.Confirm(ctx, tx.TXID)
		}
		txAdvanceProgress = func(ctx context.Context) error {
			// Update transaction log status to successful
			return t.txStore.TXSubmit(ctx, tx.TXID, true)
		}

	} else {
		confirmOrCancel = func(ctx context.Context, component TCCComponent) (*TCCResp, error) {
			// Execute second-phase cancel on the component
			return component.Cancel(ctx, tx.TXID)
		}

		txAdvanceProgress = func(ctx context.Context) error {
			// Update transaction log status to failed
			return t.txStore.TXSubmit(ctx, tx.TXID, false)
		}
	}

	for _, component := range tx.Components {
		// Retrieve the corresponding TCC component
		components, err := t.registryCenter.getComponents(component.ComponentID)
		if err != nil || len(components) == 0 {
			return errors.New("get tcc component failed")
		}
		// Execute second-phase confirm or cancel
		resp, err := confirmOrCancel(t.ctx, components[0])
		if err != nil {
			return err
		}
		if resp == nil || !resp.ACK {
			return fmt.Errorf("component: %s ack failed", component.ComponentID)
		}
	}

	// After all second-phase operations complete, submit the transaction status
	return txAdvanceProgress(t.ctx)
}

func (t *TXManager) twoPhaseCommit(ctx context.Context, txID string, componentEntities ComponentEntities) bool {
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	// Execute concurrently; on any failure, abort and cancel
	// If all succeed, batch-execute confirm and return success
	errCh := make(chan error, len(componentEntities))
	go func() {
		// Process try phase for multiple components concurrently
		var wg sync.WaitGroup
		for _, componentEntity := range componentEntities {
			// shadow
			wg.Go(func() {
				resp, err := componentEntity.Component.Try(ctx2, &TCCReq{
					ComponentID: componentEntity.Component.ID(),
					TXID:        txID,
					Data:        componentEntity.Request,
				})
				// Any component try error or rejection triggers cancel, handled in advanceProgressByTXID
				if err != nil || resp == nil || !resp.ACK {
					log.ErrorContextf(ctx2, "tx try failed, tx id: %s, component id: %s, err: %v", txID, componentEntity.Component.ID(), err)
					// Update the transaction
					if _err := t.txStore.TXUpdate(ctx2, txID, componentEntity.Component.ID(), false); _err != nil {
						log.ErrorContextf(ctx2, "tx updated failed, tx id: %s, component id: %s, err: %v", txID, componentEntity.Component.ID(), _err)
					}
					errCh <- fmt.Errorf("component: %s try failed", componentEntity.Component.ID())
					return
				}
				// If try succeeds but updating the transaction log fails, treat as failure
				if err = t.txStore.TXUpdate(ctx2, txID, componentEntity.Component.ID(), true); err != nil {
					log.ErrorContextf(ctx2, "tx updated failed, tx id: %s, component id: %s, err: %v", txID, componentEntity.Component.ID(), err)
					errCh <- err
				}
			})
		}

		wg.Wait()
		close(errCh)
	}()

	successful := true
	if err := <-errCh; err != nil {
		// If any try request fails, cancel all others
		cancel()
		successful = false
	}

	// Wait for the remaining try goroutines to finish, so that the transaction
	// log is consistent before advancing progress (they may still be writing
	// statuses concurrently).
	for range errCh {
	}

	// Execute second phase; failures are tolerated and handled by the polling task
	if err := t.advanceProgressByTXID(txID); err != nil {
		log.ErrorContextf(ctx, "advance tx progress fail, txid: %s, err: %v", txID, err)
	}
	return successful
}

func (t *TXManager) getComponents(ctx context.Context, reqs ...*RequestEntity) (ComponentEntities, error) {
	if len(reqs) == 0 {
		return nil, errors.New("empty task")
	}

	// Validate all component IDs
	idToReq := make(map[string]*RequestEntity, len(reqs))
	componentIDs := make([]string, 0, len(reqs))
	for _, req := range reqs {
		if _, ok := idToReq[req.ComponentID]; ok {
			return nil, fmt.Errorf("repeat component: %s", req.ComponentID)
		}
		idToReq[req.ComponentID] = req
		componentIDs = append(componentIDs, req.ComponentID)
	}

	// Verify validity
	components, err := t.registryCenter.getComponents(componentIDs...)
	if err != nil {
		return nil, err
	}
	if len(componentIDs) != len(components) {
		return nil, errors.New("invalid componentIDs ")
	}

	entities := make(ComponentEntities, 0, len(components))
	for _, component := range components {
		entities = append(entities, &ComponentEntity{
			Request:   idToReq[component.ID()].Request,
			Component: component,
		})
	}

	return entities, nil
}

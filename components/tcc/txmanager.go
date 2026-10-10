package tcc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/tcc/log"
)

type TXManager struct {
	ctx            context.Context
	stop           context.CancelFunc
	done           chan struct{}
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
		done:           make(chan struct{}),
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
	<-t.done
}

func (t *TXManager) Register(component TCCComponent) error {
	return t.registryCenter.register(component)
}

func (t *TXManager) Transaction(ctx context.Context, reqs ...*RequestEntity) (string, bool, error) {
	ctx2, cancel := context.WithTimeout(ctx, t.opts.Timeout)
	defer cancel()

	componentEntities, err := t.getComponents(ctx2, reqs...)
	if err != nil {
		return "", false, err
	}

	txID, err := t.txStore.CreateTX(ctx2, componentEntities.ToComponents()...)
	if err != nil {
		return "", false, err
	}

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
	defer close(t.done)
	var tick time.Duration
	var err error
	for {
		if err == nil {
			tick = t.opts.MonitorTick
		} else {
			tick = t.backOffTick(tick)
		}
		select {
		case <-t.ctx.Done():
			return

		case <-time.After(tick):
			monitorCtx, cancel := context.WithTimeout(t.ctx, t.opts.Timeout)
			if err = t.txStore.Lock(monitorCtx, t.opts.Timeout+time.Second); err != nil {
				cancel()
				err = nil
				continue
			}

			var txs []*Transaction
			if txs, err = t.txStore.GetHangingTXs(monitorCtx); err != nil {
				_ = t.txStore.Unlock(context.WithoutCancel(monitorCtx))
				cancel()
				continue
			}

			err = t.batchAdvanceProgress(monitorCtx, txs)
			_ = t.txStore.Unlock(context.WithoutCancel(monitorCtx))
			cancel()
		}
	}
}

func (t *TXManager) batchAdvanceProgress(ctx context.Context, txs []*Transaction) error {
	jobs := make(chan *Transaction)
	errs := make(chan error, len(txs))
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Go(func() {
			for tx := range jobs {
				if err := t.advanceProgress(ctx, tx); err != nil {
					errs <- err
				}
			}
		})
	}
	for _, tx := range txs {
		select {
		case jobs <- tx:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	return ctx.Err()
}

func (t *TXManager) advanceProgressByTXID(ctx context.Context, txID string) error {
	tx, err := t.txStore.GetTX(ctx, txID)
	if err != nil {
		return err
	}
	return t.advanceProgress(ctx, tx)
}

func (t *TXManager) advanceProgress(ctx context.Context, tx *Transaction) error {
	txStatus := tx.getStatus(time.Now().Add(-t.opts.Timeout))
	if txStatus == TXHanging {
		return nil
	}

	success := txStatus == TXSuccessful
	var confirmOrCancel func(ctx context.Context, component TCCComponent) (*TCCResp, error)
	var txAdvanceProgress func(ctx context.Context) error
	if success {
		confirmOrCancel = func(ctx context.Context, component TCCComponent) (*TCCResp, error) {
			return component.Confirm(ctx, tx.TXID)
		}
		txAdvanceProgress = func(ctx context.Context) error {
			return t.txStore.TXSubmit(ctx, tx.TXID, true)
		}

	} else {
		confirmOrCancel = func(ctx context.Context, component TCCComponent) (*TCCResp, error) {
			return component.Cancel(ctx, tx.TXID)
		}

		txAdvanceProgress = func(ctx context.Context) error {
			return t.txStore.TXSubmit(ctx, tx.TXID, false)
		}
	}

	for _, component := range tx.Components {
		components, err := t.registryCenter.getComponents(component.ComponentID)
		if err != nil || len(components) == 0 {
			return errors.New("get tcc component failed")
		}
		resp, err := confirmOrCancel(ctx, components[0])
		if err != nil {
			return err
		}
		if resp == nil || !resp.ACK {
			return fmt.Errorf("component: %s ack failed", component.ComponentID)
		}
	}

	return txAdvanceProgress(ctx)
}

func (t *TXManager) twoPhaseCommit(ctx context.Context, txID string, componentEntities ComponentEntities) bool {
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, len(componentEntities))
	go func() {
		var wg sync.WaitGroup
		for _, componentEntity := range componentEntities {
			wg.Go(func() {
				resp, err := componentEntity.Component.Try(ctx2, &TCCReq{
					ComponentID: componentEntity.Component.ID(),
					TXID:        txID,
					Data:        componentEntity.Request,
				})
				if err != nil || resp == nil || !resp.ACK {
					log.ErrorContextf(ctx2, "tx try failed, tx id: %s, component id: %s, err: %v", txID, componentEntity.Component.ID(), err)
					if _err := t.txStore.TXUpdate(ctx2, txID, componentEntity.Component.ID(), false); _err != nil {
						log.ErrorContextf(ctx2, "tx updated failed, tx id: %s, component id: %s, err: %v", txID, componentEntity.Component.ID(), _err)
					}
					errCh <- fmt.Errorf("component: %s try failed", componentEntity.Component.ID())
					return
				}
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
		cancel()
		successful = false
	}

	for range errCh {
	}

	commitCtx, commitCancel := context.WithTimeout(context.WithoutCancel(ctx), t.opts.Timeout)
	defer commitCancel()
	if err := t.advanceProgressByTXID(commitCtx, txID); err != nil {
		log.ErrorContextf(ctx, "advance tx progress fail, txid: %s, err: %v", txID, err)
	}
	return successful
}

func (t *TXManager) getComponents(ctx context.Context, reqs ...*RequestEntity) (ComponentEntities, error) {
	if len(reqs) == 0 {
		return nil, errors.New("empty task")
	}

	idToReq := make(map[string]*RequestEntity, len(reqs))
	componentIDs := make([]string, 0, len(reqs))
	for _, req := range reqs {
		if req == nil || req.ComponentID == "" {
			return nil, errors.New("invalid component request")
		}
		if _, ok := idToReq[req.ComponentID]; ok {
			return nil, fmt.Errorf("repeat component: %s", req.ComponentID)
		}
		idToReq[req.ComponentID] = req
		componentIDs = append(componentIDs, req.ComponentID)
	}

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

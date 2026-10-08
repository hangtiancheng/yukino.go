package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/components/redis_lock"
	"github.com/hangtiancheng/yukino.go/components/tcc"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tccStoreLockKey = "taskflow:tcc:txstore:lock"

// TXStore is the durable transaction log behind tcc.TXManager, backed by
// the tcc_tx_records table plus a redis_lock guarding the recovery monitor.
type TXStore struct {
	db          *gorm.DB
	lockFactory func(expireSeconds int64) *redis_lock.RedisLock
	lockMu      sync.Mutex
	heldLock    *redis_lock.RedisLock
}

func NewTXStore(db *gorm.DB, lockClient *redis_lock.Client) *TXStore {
	return &TXStore{
		db: db,
		lockFactory: func(expireSeconds int64) *redis_lock.RedisLock {
			return redis_lock.NewRedisLock(tccStoreLockKey, lockClient,
				redis_lock.WithExpireSeconds(expireSeconds))
		},
	}
}

type componentTryStatus struct {
	ComponentID string `json:"componentID"`
	TryStatus   string `json:"tryStatus"`
}

func (s *TXStore) CreateTX(ctx context.Context, components ...tcc.TCCComponent) (string, error) {
	statuses := make(map[string]*componentTryStatus, len(components))
	for _, c := range components {
		statuses[c.ID()] = &componentTryStatus{
			ComponentID: c.ID(),
			TryStatus:   tcc.TryHanging.String(),
		}
	}
	body, err := json.Marshal(statuses)
	if err != nil {
		return "", err
	}

	record := po.TXRecord{
		Status:               tcc.TXHanging.String(),
		ComponentTryStatuses: string(body),
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(record.ID), 10), nil
}

func (s *TXStore) TXUpdate(ctx context.Context, txID string, componentID string, accept bool) error {
	id, err := strconv.ParseUint(txID, 10, 64)
	if err != nil {
		return err
	}

	newStatus := tcc.TryFailure.String()
	if accept {
		newStatus = tcc.TrySuccessful.String()
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record po.TXRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error; err != nil {
			return err
		}

		var statuses map[string]*componentTryStatus
		if err := json.Unmarshal([]byte(record.ComponentTryStatuses), &statuses); err != nil {
			return err
		}
		status, ok := statuses[componentID]
		if !ok {
			return fmt.Errorf("unknown component %q in tx %s", componentID, txID)
		}
		if status.TryStatus == newStatus {
			return nil
		}
		if status.TryStatus != tcc.TryHanging.String() {
			return fmt.Errorf("component %q already settled (%s) in tx %s", componentID, status.TryStatus, txID)
		}

		status.TryStatus = newStatus
		body, err := json.Marshal(statuses)
		if err != nil {
			return err
		}
		record.ComponentTryStatuses = string(body)
		return tx.Model(&po.TXRecord{}).Where("id = ?", id).
			Update("component_try_statuses", string(body)).Error
	})
}

func (s *TXStore) TXSubmit(ctx context.Context, txID string, success bool) error {
	id, err := strconv.ParseUint(txID, 10, 64)
	if err != nil {
		return err
	}

	target := tcc.TXFailure.String()
	if success {
		target = tcc.TXSuccessful.String()
	}

	res := s.db.WithContext(ctx).Model(&po.TXRecord{}).
		Where("id = ? AND status = ?", id, tcc.TXHanging.String()).
		Update("status", target)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		return nil
	}

	var record po.TXRecord
	if err := s.db.WithContext(ctx).Select("status").First(&record, id).Error; err != nil {
		return err
	}
	if record.Status == target {
		return nil
	}
	return fmt.Errorf("tx %s already submitted as %s", txID, record.Status)
}

func (s *TXStore) GetHangingTXs(ctx context.Context) ([]*tcc.Transaction, error) {
	var records []po.TXRecord
	err := s.db.WithContext(ctx).
		Where("status = ?", tcc.TXHanging.String()).
		Order("id ASC").Limit(200).Find(&records).Error
	if err != nil {
		return nil, err
	}

	txs := make([]*tcc.Transaction, 0, len(records))
	for _, record := range records {
		tx, err := recordToTransaction(&record)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func (s *TXStore) GetTX(ctx context.Context, txID string) (*tcc.Transaction, error) {
	id, err := strconv.ParseUint(txID, 10, 64)
	if err != nil {
		return nil, err
	}
	var record po.TXRecord
	if err := s.db.WithContext(ctx).First(&record, id).Error; err != nil {
		return nil, err
	}
	return recordToTransaction(&record)
}

func recordToTransaction(record *po.TXRecord) (*tcc.Transaction, error) {
	var statuses map[string]*componentTryStatus
	if err := json.Unmarshal([]byte(record.ComponentTryStatuses), &statuses); err != nil {
		return nil, err
	}

	components := make([]*tcc.ComponentTryEntity, 0, len(statuses))
	for _, status := range statuses {
		components = append(components, &tcc.ComponentTryEntity{
			ComponentID: status.ComponentID,
			TryStatus:   tcc.ComponentTryStatus(status.TryStatus),
		})
	}

	txStatus := tcc.TXHanging
	switch record.Status {
	case tcc.TXSuccessful.String():
		txStatus = tcc.TXSuccessful
	case tcc.TXFailure.String():
		txStatus = tcc.TXFailure
	}

	return &tcc.Transaction{
		TXID:       strconv.FormatUint(uint64(record.ID), 10),
		Status:     txStatus,
		CreatedAt:  record.CreatedAt,
		Components: components,
	}, nil
}

func (s *TXStore) Lock(ctx context.Context, expireDuration time.Duration) error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if s.heldLock != nil {
		return fmt.Errorf("transaction store lock already held")
	}
	seconds := int64(expireDuration.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	lock := s.lockFactory(seconds)
	if err := lock.Lock(ctx); err != nil {
		return err
	}
	s.heldLock = lock
	return nil
}

func (s *TXStore) Unlock(ctx context.Context) error {
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	if s.heldLock == nil {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	err := s.heldLock.Unlock(cleanup)
	s.heldLock = nil
	return err
}

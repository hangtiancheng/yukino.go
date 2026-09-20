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

package dao

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TXRecordPO struct {
	gorm.Model
	Status               string `gorm:"status"`
	ComponentTryStatuses string `gorm:"component_try_statuses"`
}

func (t TXRecordPO) TableName() string {
	return "tx_record"
}

type ComponentTryStatus struct {
	ComponentID string `json:"componentID"`
	TryStatus   string `json:"tryStatus"`
}

type TXRecordDAO struct {
	db *gorm.DB
}

// TXRecordUpdater is the subset of TXRecordDAO used inside LockAndDo callbacks.
type TXRecordUpdater interface {
	UpdateTXRecord(ctx context.Context, record *TXRecordPO) error
}

func NewTXRecordDAO(db *gorm.DB) *TXRecordDAO {
	return &TXRecordDAO{
		db: db,
	}
}

func (t *TXRecordDAO) GetTXRecords(ctx context.Context, opts ...QueryOption) ([]*TXRecordPO, error) {
	db := t.db.WithContext(ctx).Model(&TXRecordPO{})
	for _, opt := range opts {
		db = opt(db)
	}

	var records []*TXRecordPO
	return records, db.Scan(&records).Error
}

func (t *TXRecordDAO) CreateTXRecord(ctx context.Context, record *TXRecordPO) (uint, error) {
	return record.ID, t.db.WithContext(ctx).Model(&TXRecordPO{}).Create(record).Error
}

func (t *TXRecordDAO) UpdateComponentStatus(ctx context.Context, id uint, componentID string, status string) error {
	return t.LockAndDo(ctx, id, func(ctx context.Context, dao TXRecordUpdater, record *TXRecordPO) error {
		var statuses map[string]*ComponentTryStatus
		if err := json.Unmarshal([]byte(record.ComponentTryStatuses), &statuses); err != nil {
			return err
		}

		componentStatus, ok := statuses[componentID]
		if !ok {
			return fmt.Errorf("invalid component: %s in txid: %d", componentID, id)
		}
		if componentStatus.TryStatus == status {
			return nil
		}

		if componentStatus.TryStatus == tcc_demo.TryHanging.String() {
			componentStatus.TryStatus = status
			body, _ := json.Marshal(statuses)
			record.ComponentTryStatuses = string(body)
			return dao.UpdateTXRecord(ctx, record)
		}

		return fmt.Errorf("invalid status: %s of component: %s, txid: %d", statuses[componentID].TryStatus, componentID, id)
	})
}

func (t *TXRecordDAO) UpdateTXRecord(ctx context.Context, record *TXRecordPO) error {
	return t.db.WithContext(ctx).Updates(record).Error
}

func (t *TXRecordDAO) LockAndDo(ctx context.Context, id uint, do func(ctx context.Context, dao TXRecordUpdater, record *TXRecordPO) error) error {
	return t.db.Transaction(func(tx *gorm.DB) error {
		// Acquire write lock
		var record TXRecordPO

		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, id).Error; err != nil {
			return err
		}

		txDAO := NewTXRecordDAO(tx)
		return do(ctx, txDAO, &record)
	})
}

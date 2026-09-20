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
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Test_GetTXRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	mock.ExpectQuery("SELECT VERSION()").WillReturnRows(sqlmock.NewRows([]string{"VERSION"}).AddRow("1"))

	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn: db,
	}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Error(err)
		return
	}

	ctx := context.Background()
	txRecordDAO := NewTXRecordDAO(gdb)

	tests := []struct {
		name string
		f    func()
	}{
		{
			name: "GetTXRecords",
			f: func() {
				componentStatus := map[string]*ComponentTryStatus{
					"component_a": {
						ComponentID: "component_a",
						TryStatus:   tcc_demo.TryHanging.String(),
					},
				}
				body, _ := json.Marshal(componentStatus)

				now := time.Now()
				rows := sqlmock.NewRows([]string{"id", "create_at", "deleted_at", "updated_at", "status", "component_try_statuses"}).AddRow(1, now, now, now, tcc_demo.TXHanging.String(), string(body))
				mock.ExpectQuery("SELECT \\* FROM `tx_record` WHERE id = \\? AND status = \\? AND `tx_record`.`deleted_at` IS NULL").WithArgs(1, tcc_demo.TXHanging.String()).WillReturnRows(rows)
				txRecords, err := txRecordDAO.GetTXRecords(ctx, WithID(1), WithStatus(tcc_demo.ComponentTryStatus(tcc_demo.TXHanging.String())))
				if err != nil {
					t.Error(err)
					return
				}
				if len(txRecords) != 1 {
					t.Errorf("expected 1, got %d", len(txRecords))
				}
				if txRecords[0].ID != uint(1) {
					t.Errorf("expected 1, got %d", txRecords[0].ID)
				}
				if txRecords[0].Status != tcc_demo.TXHanging.String() {
					t.Errorf("expected %s, got %s", tcc_demo.TXHanging.String(), txRecords[0].Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.f()
		})
	}
}

func Test_CreateTXRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	mock.ExpectQuery("SELECT VERSION()").WillReturnRows(sqlmock.NewRows([]string{"VERSION"}).AddRow("1"))

	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn: db,
	}), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Error(err)
		return
	}

	ctx := context.Background()
	txRecordDAO := NewTXRecordDAO(gdb)

	tests := []struct {
		name string
		f    func()
	}{
		{
			name: "CreateTXRecord",
			f: func() {
				componentStatus := map[string]*ComponentTryStatus{
					"component_a": {
						ComponentID: "component_a",
						TryStatus:   tcc_demo.TryHanging.String(),
					},
				}
				body, _ := json.Marshal(componentStatus)

				now := time.Now()
				mock.ExpectBegin()
				mock.ExpectExec("INSERT INTO `tx_record`").WithArgs(now, now, nil, tcc_demo.TXHanging.String(), string(body)).WillReturnResult(driver.ResultNoRows)
				mock.ExpectCommit()
				_, err := txRecordDAO.CreateTXRecord(ctx, &TXRecordPO{
					Status:               tcc_demo.TXHanging.String(),
					ComponentTryStatuses: string(body),
					Model: gorm.Model{
						CreatedAt: now,
						UpdatedAt: now,
					},
				})
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.f()
		})
	}
}

func Test_UpdateComponentStatus(t *testing.T) {
	now := time.Now()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	mock.ExpectQuery("SELECT VERSION()").WillReturnRows(sqlmock.NewRows([]string{"VERSION"}).AddRow("1"))

	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn: db,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Error(err)
		return
	}

	ctx := context.Background()
	txRecordDAO := NewTXRecordDAO(gdb)

	tests := []struct {
		name string
		f    func()
	}{
		{
			name: "UpdateComponentStatusSuccess",
			f: func() {
				componentStatus := map[string]*ComponentTryStatus{
					"component_a": {
						ComponentID: "component_a",
						TryStatus:   tcc_demo.TryHanging.String(),
					},
				}
				body, _ := json.Marshal(componentStatus)

				mock.ExpectBegin()

				rows := sqlmock.NewRows([]string{"id", "create_at", "deleted_at", "updated_at", "status", "component_try_statuses"}).AddRow(1, now, nil, now, tcc_demo.TXHanging.String(), string(body))
				mock.ExpectQuery("SELECT \\* FROM `tx_record` WHERE `tx_record`.`id` = \\? AND `tx_record`.`deleted_at` IS NULL ORDER BY `tx_record`.`id` LIMIT \\? FOR UPDATE").WithArgs(1, 1).WillReturnRows(rows)

				newComponentStatus := map[string]*ComponentTryStatus{
					"component_a": {
						ComponentID: "component_a",
						TryStatus:   tcc_demo.TrySuccessful.String(),
					},
				}
				newBody, _ := json.Marshal(newComponentStatus)
				mock.ExpectExec("UPDATE `tx_record` SET `updated_at`=\\?,`status`=\\?,`component_try_statuses`=\\? WHERE `tx_record`.`deleted_at` IS NULL AND `id` = \\?").WithArgs(now, tcc_demo.TXHanging.String(), string(newBody), 1).WillReturnResult(driver.ResultNoRows)
				mock.ExpectCommit()
				err := txRecordDAO.UpdateComponentStatus(ctx, 1, "component_a", tcc_demo.TrySuccessful.String())
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
			},
		},

		{
			name: "UpdateComponentStatusMissComponent",
			f: func() {
				componentStatus := map[string]*ComponentTryStatus{
					"component_a": {
						ComponentID: "component_a",
						TryStatus:   tcc_demo.TryHanging.String(),
					},
				}
				body, _ := json.Marshal(componentStatus)

				mock.ExpectBegin()

				rows := sqlmock.NewRows([]string{"id", "create_at", "deleted_at", "updated_at", "status", "component_try_statuses"}).AddRow(1, now, nil, now, tcc_demo.TXHanging.String(), string(body))
				mock.ExpectQuery("SELECT \\* FROM `tx_record` WHERE `tx_record`.`id` = \\? AND `tx_record`.`deleted_at` IS NULL ORDER BY `tx_record`.`id` LIMIT \\? FOR UPDATE").WithArgs(1, 1).WillReturnRows(rows)

				mock.ExpectRollback()
				err := txRecordDAO.UpdateComponentStatus(ctx, 1, "component_b", tcc_demo.TrySuccessful.String())
				if err == nil {
					t.Error("expected error, got nil")
				}
			},
		},
		{
			name: "UpdateComponentStatusStatusFail",
			f: func() {
				componentStatus := map[string]*ComponentTryStatus{
					"component_a": {
						ComponentID: "component_a",
						TryStatus:   tcc_demo.TryFailure.String(),
					},
				}
				body, _ := json.Marshal(componentStatus)

				mock.ExpectBegin()

				rows := sqlmock.NewRows([]string{"id", "create_at", "deleted_at", "updated_at", "status", "component_try_statuses"}).AddRow(1, now, nil, now, tcc_demo.TXHanging.String(), string(body))
				mock.ExpectQuery("SELECT \\* FROM `tx_record` WHERE `tx_record`.`id` = \\? AND `tx_record`.`deleted_at` IS NULL ORDER BY `tx_record`.`id` LIMIT \\? FOR UPDATE").WithArgs(1, 1).WillReturnRows(rows)

				mock.ExpectCommit()
				err := txRecordDAO.UpdateComponentStatus(ctx, 1, "component_a", tcc_demo.TrySuccessful.String())
				if err == nil {
					t.Error("expected error, got nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.f()
		})
	}
}

func Test_UpdateTXRecord(t *testing.T) {
	now := time.Now()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Error(err)
		return
	}
	defer db.Close()
	mock.ExpectQuery("SELECT VERSION()").WillReturnRows(sqlmock.NewRows([]string{"VERSION"}).AddRow("1"))

	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn: db,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		NowFunc: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Error(err)
		return
	}

	ctx := context.Background()
	txRecordDAO := NewTXRecordDAO(gdb)

	tests := []struct {
		name string
		f    func()
	}{
		{
			name: "UpdateTXRecord",
			f: func() {
				mock.ExpectBegin()
				mock.ExpectExec("UPDATE `tx_record` SET `updated_at`=\\?,`status`=\\?,`component_try_statuses`=\\? WHERE `tx_record`.`deleted_at` IS NULL AND `id` = \\?").WithArgs(now, tcc_demo.TXHanging.String(), "{}", 1).WillReturnResult(driver.ResultNoRows)
				mock.ExpectCommit()
				err := txRecordDAO.UpdateTXRecord(ctx, &TXRecordPO{
					Model: gorm.Model{
						ID: 1,
					},
					Status:               tcc_demo.TXHanging.String(),
					ComponentTryStatuses: "{}",
				})
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.f()
		})
	}
}

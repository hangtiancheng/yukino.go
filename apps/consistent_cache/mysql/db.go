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

package mysql

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/hangtiancheng/yukino.go/apps/consistent_cache"
)

type tableInterface interface {
	TableName() string
}

// DB implements consistent_cache.DB backed by gorm.
type DB struct {
	db *gorm.DB
}

func NewDB(dsn string) *DB {
	return &DB{db: getDB(dsn)}
}

// Put writes obj to the database. It emulates upsert: try insert first, fall back to update on unique-key conflict.
func (d *DB) Put(ctx context.Context, obj consistent_cache.Object) error {
	db := d.db
	tableInst, ok := obj.(tableInterface)
	if ok {
		db = db.Table(tableInst.TableName())
	}

	err := db.WithContext(ctx).Create(obj).Error
	if err == nil {
		return nil
	}

	if IsDuplicateEntryErr(err) {
		return db.WithContext(ctx).Debug().Where(fmt.Sprintf("`%s` = ?", obj.KeyColumn()), obj.Key()).Updates(obj).Error
	}
	return err
}

// Get loads obj from the database by its key column.
func (d *DB) Get(ctx context.Context, obj consistent_cache.Object) error {
	db := d.db
	tableInst, ok := obj.(tableInterface)
	if ok {
		db = db.Table(tableInst.TableName())
	}

	err := db.WithContext(ctx).Where(fmt.Sprintf("`%s` = ?", obj.KeyColumn()), obj.Key()).First(obj).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return consistent_cache.ErrorDBMiss
	}
	return err
}

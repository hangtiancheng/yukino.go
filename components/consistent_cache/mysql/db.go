package mysql

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/hangtiancheng/yukino.go/components/consistent_cache"
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

// NewWithGorm reuses a caller-owned pool; the caller retains close ownership.
func NewWithGorm(db *gorm.DB) *DB { return &DB{db: db} }

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

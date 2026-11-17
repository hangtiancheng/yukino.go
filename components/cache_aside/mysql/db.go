package mysql

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/hangtiancheng/yukino.go/components/cache_aside"
)

type tableInterface interface {
	TableName() string
}

type DB struct {
	db *gorm.DB
}

func NewDB(dsn string) *DB {
	return &DB{db: getDB(dsn)}
}

func NewWithGorm(db *gorm.DB) *DB { return &DB{db: db} }

func (d *DB) Put(ctx context.Context, obj cache_aside.Object) error {
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

func (d *DB) Get(ctx context.Context, obj cache_aside.Object) error {
	db := d.db
	tableInst, ok := obj.(tableInterface)
	if ok {
		db = db.Table(tableInst.TableName())
	}

	err := db.WithContext(ctx).Where(fmt.Sprintf("`%s` = ?", obj.KeyColumn()), obj.Key()).First(obj).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cache_aside.ErrorDBMiss
	}
	return err
}

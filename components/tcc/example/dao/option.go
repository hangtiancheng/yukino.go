package dao

import (
	"github.com/hangtiancheng/yukino.go/components/tcc"
	"gorm.io/gorm"
)

type QueryOption func(db *gorm.DB) *gorm.DB

func WithID(id uint) QueryOption {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	}
}

func WithStatus(status tcc.ComponentTryStatus) QueryOption {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("status = ?", status.String())
	}
}

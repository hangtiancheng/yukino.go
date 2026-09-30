package dao

import (
	"github.com/hangtiancheng/yukino.go/apps/tcc_demo"
	"gorm.io/gorm"
)

type QueryOption func(db *gorm.DB) *gorm.DB

func WithID(id uint) QueryOption {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("id = ?", id)
	}
}

func WithStatus(status tcc_demo.ComponentTryStatus) QueryOption {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("status = ?", status.String())
	}
}

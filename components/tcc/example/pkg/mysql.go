package pkg

import (
	"fmt"
	"sync"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const dsn = ""

// DBFactory abstracts database creation for testability.
type DBFactory interface {
	Open(dsn string, opts ...gorm.Option) (*gorm.DB, error)
}

type defaultDBFactory struct{}

func (defaultDBFactory) Open(dsn string, opts ...gorm.Option) (*gorm.DB, error) {
	return gorm.Open(mysql.Open(dsn), opts...)
}

var (
	dbFactory DBFactory = defaultDBFactory{}
	db        *gorm.DB
	debounce  sync.Once
)

func NewDB(dsn string, opts ...gorm.Option) (*gorm.DB, error) {
	return dbFactory.Open(dsn, opts...)
}

func GetDB() *gorm.DB {
	debounce.Do(func() {
		var err error
		if db, err = dbFactory.Open(dsn, &gorm.Config{}); err != nil {
			panic(fmt.Errorf("failed to connect database, err: %w", err))
		}
	})
	return db
}

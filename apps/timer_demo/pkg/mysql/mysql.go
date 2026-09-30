package mysql

import (
	"errors"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/conf"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const DuplicateEntryErrCode = 1062

// Client is a MySQL database client wrapping gorm.DB.
type Client struct {
	*gorm.DB
}

// GetClient creates a new database client from configuration.
func GetClient(confProvider *conf.MysqlConfProvider) (*Client, error) {
	conf := confProvider.Get()
	db, err := gorm.Open(mysql.Open(conf.DSN), &gorm.Config{TranslateError: true})
	if err != nil {
		panic(fmt.Errorf("failed to connect database, err: %w", err))
	}
	_db, err := db.DB()
	if err != nil {
		panic(err)
	}
	_db.SetMaxOpenConns(conf.MaxOpenConns)
	_db.SetMaxIdleConns(conf.MaxIdleConns)
	return &Client{DB: db}, nil
}

func NewClient(db *gorm.DB) *Client {
	return &Client{
		DB: db,
	}
}

func IsDuplicateEntryErr(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}

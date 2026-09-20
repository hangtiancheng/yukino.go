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

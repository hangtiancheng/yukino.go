package mysql

import (
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const DuplicateEntryErrCode = 1062

func getDB(dsn string) *gorm.DB {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return db
}

func IsDuplicateEntryErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Error 1062")
}

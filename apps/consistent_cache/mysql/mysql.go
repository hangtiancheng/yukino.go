package mysql

import (
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// DuplicateEntryErrCode is the MySQL error number for unique-key conflicts.
const DuplicateEntryErrCode = 1062

// getDB opens a gorm DB connection against the given DSN.
func getDB(dsn string) *gorm.DB {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return db
}

// IsDuplicateEntryErr reports whether err is a MySQL duplicate-entry (unique-key conflict) error.
// It matches on error number to avoid importing the mysql driver package directly.
func IsDuplicateEntryErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Error 1062")
}

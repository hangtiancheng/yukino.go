package model

import "gorm.io/gorm"

// User is the user data model. Password stores the bcrypt hash and is never serialized to JSON.
//
// ⚠️ created_at/updated_at use int64 (Unix seconds, autoCreateTime/autoUpdateTime),
// aligned with base-server's userRow (both share the same remote users table).
// Do not use gorm.Model (its CreatedAt/UpdatedAt are time.Time → datetime(3));
// otherwise AutoMigrate would change the bigint-aligned columns back to datetime, hitting
// Error 1292 under strict mode when bigint data already exists.
type User struct {
	ID        uint           `gorm:"column:id;primaryKey;autoIncrement"`
	Username  string         `gorm:"column:username;uniqueIndex;size:64;not null" json:"username"`
	Password  string         `gorm:"column:password;size:255;not null" json:"-"`
	Email     string         `gorm:"column:email;uniqueIndex;size:128;not null" json:"email"`
	CreatedAt int64          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt int64          `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

// TableName specifies the table name.
func (User) TableName() string { return "users" }

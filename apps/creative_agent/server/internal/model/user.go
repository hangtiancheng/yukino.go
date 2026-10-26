package model

import "gorm.io/gorm"

// User 是用户数据模型。Password 存 bcrypt 哈希，永不序列化到 JSON。
//
// ⚠️ created_at/updated_at 用 int64（Unix 秒，autoCreateTime/autoUpdateTime），
// 对齐 base-server 的 userRow（两者共用同一张远程 users 表）。
// 不要用 gorm.Model（其 CreatedAt/UpdatedAt 是 time.Time → datetime(3)），
// 否则 AutoMigrate 会把已对齐成 bigint 的列改回 datetime，撞上严格模式下
// 已有 bigint 数据报错（Error 1292）。
type User struct {
	ID        uint           `gorm:"column:id;primaryKey;autoIncrement"`
	Username  string         `gorm:"column:username;uniqueIndex;size:64;not null" json:"username"`
	Password  string         `gorm:"column:password;size:255;not null" json:"-"`
	Email     string         `gorm:"column:email;uniqueIndex;size:128;not null" json:"email"`
	CreatedAt int64          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt int64          `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

// TableName 指定表名。
func (User) TableName() string { return "users" }

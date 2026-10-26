package domain

import (
	"context"
	"errors"
)

// ErrUserNotFound 表示未找到对应用户记录。与 legacy repository 的哨兵错误等价。
var ErrUserNotFound = errors.New("user not found")

// UserRepository 用户仓储接口（domain 定义，infrastructure 实现）。
type UserRepository interface {
	// Create 新建用户。唯一索引（username/email）冲突时返回 gorm.ErrDuplicatedKey。
	Create(ctx context.Context, u *User) error
	// FindByUsername 按用户名查询，未找到返回 ErrUserNotFound。
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByEmail 按邮箱查询，未找到返回 ErrUserNotFound。
	FindByEmail(ctx context.Context, email string) (*User, error)
}

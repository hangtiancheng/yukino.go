package repository

import (
	"context"
	"errors"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrUserNotFound 表示未找到对应用户记录。
var ErrUserNotFound = errors.New("user not found")

// UserRepository 封装用户表的数据访问，供 service 依赖（便于 mock）。
type UserRepository interface {
	Create(ctx context.Context, u *model.User) error
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
}

type userRepository struct {
	db *gorm.DB
}

// NewUserRepository 基于 gorm.DB 创建 repository（原生 GORM 查询）。
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *userRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	return r.findOne(ctx, "username = ?", username)
}

func (r *userRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	return r.findOne(ctx, "email = ?", email)
}

func (r *userRepository) findOne(ctx context.Context, query string, args ...any) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where(query, args...).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

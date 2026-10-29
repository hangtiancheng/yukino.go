package repository

import (
	"context"
	"errors"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrUserNotFound indicates no matching user record was found.
var ErrUserNotFound = errors.New("user not found")

// UserRepository encapsulates data access to the users table for the service layer to depend on (easy to mock).
type UserRepository interface {
	Create(ctx context.Context, u *model.User) error
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
}

type userRepository struct {
	db *gorm.DB
}

// NewUserRepository creates the repository on top of gorm.DB (native GORM queries).
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

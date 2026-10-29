package domain

import (
	"context"
	"errors"
)

// ErrUserNotFound indicates no matching user record was found. Equivalent to the legacy repository's sentinel error.
var ErrUserNotFound = errors.New("user not found")

// UserRepository is the user repository interface (defined in domain, implemented in infrastructure).
type UserRepository interface {
	// Create creates a user. Unique-index (username/email) conflicts return gorm.ErrDuplicatedKey.
	Create(ctx context.Context, u *User) error
	// FindByUsername queries by username; returns ErrUserNotFound when absent.
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByEmail queries by email; returns ErrUserNotFound when absent.
	FindByEmail(ctx context.Context, email string) (*User, error)
}

package infrastructure

import (
	"context"
	"errors"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/auth/domain"

	"gorm.io/gorm"
)

// userRow is the users-table persistence structure (gorm mapping the users table).
// Private: gorm tags live only in the infrastructure layer; the domain entity stays pure.
//
// Field types are aligned with the real users table (verified 2026-08 via SHOW COLUMNS FROM users):
//   - id            bigint unsigned PK AUTO_INCREMENT
//   - created_at    bigint   ← the old gorm.Model datetime was migrated to bigint
//   - updated_at    bigint   (kept int64 = Unix seconds after the Task 5.2 AutoMigrate alignment)
//   - deleted_at    datetime(3) ← consistent with gorm.DeletedAt's default mapping
type userRow struct {
	ID        uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	Username  string         `gorm:"column:username;uniqueIndex;size:64;not null"`
	Password  string         `gorm:"column:password;size:255;not null"`
	Email     string         `gorm:"column:email;uniqueIndex;size:128;not null"`
	CreatedAt int64          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt int64          `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

// TableName specifies the table name.
func (userRow) TableName() string { return "users" }

// Migrate runs AutoMigrate for this context's tables (called at database.Init startup).
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&userRow{})
}

// toRow converts the domain entity into the persistence structure.
func toRow(u *domain.User) *userRow {
	return &userRow{
		ID:        u.ID,
		Username:  u.Username,
		Password:  u.PasswordHash,
		Email:     u.Email,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// toDomain converts the persistence structure into the domain entity.
func toDomain(r *userRow) *domain.User {
	return &domain.User{
		ID:           r.ID,
		Username:     r.Username,
		PasswordHash: r.Password,
		Email:        r.Email,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
}

// UserRepositoryImpl is the gorm-based domain.UserRepository implementation (bare gorm, no gen dependency).
type UserRepositoryImpl struct {
	db *gorm.DB
}

// NewUserRepositoryImpl creates the implementation.
func NewUserRepositoryImpl(db *gorm.DB) *UserRepositoryImpl {
	return &UserRepositoryImpl{db: db}
}

// Compile-time assertion: the implementation satisfies the domain.UserRepository interface.
var _ domain.UserRepository = (*UserRepositoryImpl)(nil)

// Create creates a user. Unique-index (username/email) conflicts return gorm.ErrDuplicatedKey.
func (r *UserRepositoryImpl) Create(ctx context.Context, u *domain.User) error {
	row := toRow(u)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	// Backfill the auto-increment ID and the DB-written timestamps.
	u.ID = row.ID
	u.CreatedAt = row.CreatedAt
	u.UpdatedAt = row.UpdatedAt
	return nil
}

// FindByUsername queries by username; returns domain.ErrUserNotFound when absent.
func (r *UserRepositoryImpl) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	var row userRow
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(&row), nil
}

// FindByEmail queries by email; returns domain.ErrUserNotFound when absent.
func (r *UserRepositoryImpl) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	var row userRow
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return toDomain(&row), nil
}

package infrastructure

import (
	"context"
	"errors"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/auth/domain"

	"gorm.io/gorm"
)

// userRow 用户表持久化结构（gorm 映射 users 表）。
// 私有：gorm 标签只存在于基础设施层，域实体保持纯领域。
//
// 字段类型已对齐真实 users 表（2026-08 实测 SHOW COLUMNS FROM users）：
//   - id            bigint unsigned PK AUTO_INCREMENT
//   - created_at    bigint   ← 旧 gorm.Model 的 datetime 已被迁移成 bigint
//   - updated_at    bigint   （Task 5.2 AutoMigrate 对齐后保持 int64 = Unix 秒）
//   - deleted_at    datetime(3) ← 与 gorm.DeletedAt 默认映射一致
type userRow struct {
	ID        uint64         `gorm:"column:id;primaryKey;autoIncrement"`
	Username  string         `gorm:"column:username;uniqueIndex;size:64;not null"`
	Password  string         `gorm:"column:password;size:255;not null"`
	Email     string         `gorm:"column:email;uniqueIndex;size:128;not null"`
	CreatedAt int64          `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt int64          `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

// TableName 指定表名。
func (userRow) TableName() string { return "users" }

// Migrate 对本 context 的表执行 AutoMigrate（database.Init 启动时调用）。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&userRow{})
}

// toRow 将域实体转为持久化结构。
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

// toDomain 将持久化结构转为域实体。
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

// UserRepositoryImpl 基于 gorm 的 domain.UserRepository 实现（bare gorm，无 gen 依赖）。
type UserRepositoryImpl struct {
	db *gorm.DB
}

// NewUserRepositoryImpl 创建实现。
func NewUserRepositoryImpl(db *gorm.DB) *UserRepositoryImpl {
	return &UserRepositoryImpl{db: db}
}

// 编译期断言：确保实现满足 domain.UserRepository 接口。
var _ domain.UserRepository = (*UserRepositoryImpl)(nil)

// Create 新建用户。唯一索引（username/email）冲突时返回 gorm.ErrDuplicatedKey。
func (r *UserRepositoryImpl) Create(ctx context.Context, u *domain.User) error {
	row := toRow(u)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	// 回填自增 ID 与数据库写入的时间戳。
	u.ID = row.ID
	u.CreatedAt = row.CreatedAt
	u.UpdatedAt = row.UpdatedAt
	return nil
}

// FindByUsername 按用户名查询，未找到返回 domain.ErrUserNotFound。
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

// FindByEmail 按邮箱查询，未找到返回 domain.ErrUserNotFound。
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

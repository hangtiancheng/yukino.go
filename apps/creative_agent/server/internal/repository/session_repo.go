package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrSessionNotFound indicates no matching session record was found.
var ErrSessionNotFound = errors.New("session not found")

// SessionRepository encapsulates data access to the cg_session table.
type SessionRepository interface {
	Insert(ctx context.Context, s *model.CGSession) error
	GetBySessionID(ctx context.Context, sessionID string) (*model.CGSession, error)
	UpdateBySessionID(ctx context.Context, sessionID string, assigns map[string]any) error
}

type sessionRepository struct {
	db *gorm.DB
}

// NewSessionRepository creates a SessionRepository on top of gorm.DB.
func NewSessionRepository(db *gorm.DB) SessionRepository {
	return &sessionRepository{db: db}
}

func (r *sessionRepository) Insert(ctx context.Context, s *model.CGSession) error {
	now := time.Now().UnixMilli()
	if s.CreateTime == 0 {
		s.CreateTime = now
	}
	if s.UpdateTime == 0 {
		s.UpdateTime = now
	}
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *sessionRepository) GetBySessionID(ctx context.Context, sessionID string) (*model.CGSession, error) {
	var s model.CGSession
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *sessionRepository) UpdateBySessionID(ctx context.Context, sessionID string, assigns map[string]any) error {
	assigns["update_time"] = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&model.CGSession{}).
		Where("session_id = ?", sessionID).
		Updates(assigns).Error
}

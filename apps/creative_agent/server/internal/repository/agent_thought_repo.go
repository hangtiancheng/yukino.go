package repository

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// AgentThoughtRepository encapsulates data access to the cg_agent_thought table.
type AgentThoughtRepository interface {
	// Insert inserts one thought record and returns the auto-increment primary key id.
	Insert(ctx context.Context, t *model.CGAgentThought) (int64, error)
	// UpdateByID updates fields by primary key.
	UpdateByID(ctx context.Context, id int64, assigns map[string]any) error
}

type agentThoughtRepository struct {
	db *gorm.DB
}

// NewAgentThoughtRepository creates an AgentThoughtRepository on top of gorm.DB.
func NewAgentThoughtRepository(db *gorm.DB) AgentThoughtRepository {
	return &agentThoughtRepository{db: db}
}

func (r *agentThoughtRepository) Insert(ctx context.Context, t *model.CGAgentThought) (int64, error) {
	now := time.Now().UnixMilli()
	if t.CreateTime == 0 {
		t.CreateTime = now
	}
	if t.UpdateTime == 0 {
		t.UpdateTime = now
	}
	if err := r.db.WithContext(ctx).Create(t).Error; err != nil {
		return 0, err
	}
	return t.ID, nil
}

func (r *agentThoughtRepository) UpdateByID(ctx context.Context, id int64, assigns map[string]any) error {
	assigns["update_time"] = time.Now().UnixMilli()
	return r.db.WithContext(ctx).
		Model(&model.CGAgentThought{}).
		Where("id = ?", id).
		Updates(assigns).Error
}

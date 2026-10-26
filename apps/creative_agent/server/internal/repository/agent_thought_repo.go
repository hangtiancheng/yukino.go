package repository

import (
	"context"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// AgentThoughtRepository 封装 cg_agent_thought 表的数据访问。
type AgentThoughtRepository interface {
	// Insert 插入一条 thought 记录，返回自增主键 id。
	Insert(ctx context.Context, t *model.CGAgentThought) (int64, error)
	// UpdateByID 按主键更新字段。
	UpdateByID(ctx context.Context, id int64, assigns map[string]any) error
}

type agentThoughtRepository struct {
	db *gorm.DB
}

// NewAgentThoughtRepository 基于 gorm.DB 创建 AgentThoughtRepository。
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

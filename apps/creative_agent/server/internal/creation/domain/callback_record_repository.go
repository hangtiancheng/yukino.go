package domain

import (
	"context"
	"errors"
)

// ErrCallbackRecordNotFound 表示未找到对应回调记录。
var ErrCallbackRecordNotFound = errors.New("callback record not found")

// CallbackRecordRepository 任务回调记录仓储接口。
type CallbackRecordRepository interface {
	// Insert 插入回调记录，返回自增 ID。
	Insert(ctx context.Context, r *CallbackRecord) (uint64, error)
	// Get 按主键查回调记录。
	Get(ctx context.Context, id uint64) (*CallbackRecord, error)
	// GetByProjectAndType 按 project + task_type 查回调记录（幂等 / 对账用）。
	GetByProjectAndType(ctx context.Context, projectID, taskType string) (*CallbackRecord, error)
	// Update 按主键更新回调记录的指定字段（仅非终态字段）。
	Update(ctx context.Context, id uint64, assigns map[string]any) error
	// MarkStatusIf 乐观锁：仅当当前 status 在 allowedFrom 内才置新 status。
	// 返回是否更新成功（影响行数=1）。
	//
	// 调用方约定：状态迁移必须显式传 allowedFrom（回调重放幂等依赖它）；
	// 传空 allowedFrom 时实现方将无条件更新（无乐观锁保护），仅限非终态迁移场景使用。
	MarkStatusIf(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error)
}

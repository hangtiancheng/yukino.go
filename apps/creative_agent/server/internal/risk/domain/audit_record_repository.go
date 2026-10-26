package domain

import (
	"context"
	"errors"
)

// ErrRecordNotFound 表示未找到对应审计记录。
var ErrRecordNotFound = errors.New("audit record not found")

// AuditRecordRepository 风控审计记录仓储接口。
//
// 说明：Update 用 map[string]any 仍是技术细节泄漏，重构目标应尽量收敛；
// 但审计记录更新点分散（task_input 写入、终态落定、callback_url），
// 先用 map 保持与现状等价，后续可细化。
type AuditRecordRepository interface {
	// Insert 插入审计记录，返回自增 ID。
	Insert(ctx context.Context, r *AuditRecord) (uint64, error)
	// Get 按主键查。
	Get(ctx context.Context, id uint64) (*AuditRecord, error)
	// Update 按主键更新指定字段（仅非终态字段）。
	Update(ctx context.Context, id uint64, assigns map[string]any) error
	// MarkTerminal 乐观锁置终态：仅当 status 在 allowedFrom 内才更新。
	// 返回是否更新成功（影响行数=1）。
	//
	// 调用方约定：终态迁移必须显式传 allowedFrom（回调重放幂等依赖它）；
	// 传空 allowedFrom 时实现方将无条件更新（无乐观锁保护），仅限非终态迁移场景使用。
	MarkTerminal(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error)
}

package domain

import (
	"context"
	"errors"
)

// 补偿业务类型（阶段 5 的补偿 job 按此分类重试）。
const (
	BizTypeAsyncAudit    = 1 // 异步风控提交失败
	BizTypeCallbackAgent = 2 // 回调 agent 失败
)

// 补偿记录状态（cg_audit_compensation.status）。
const (
	CompensationStatusPending = 0 // 待处理（job 会扫描重试）
	CompensationStatusDone    = 1 // 已处理（重试成功 / 超上限放弃）
)

// ErrCompensationNotFound 表示未找到对应补偿记录。
var ErrCompensationNotFound = errors.New("compensation not found")

// Compensation 补偿记录（cg_audit_compensation）。
//
// AsyncAudit 提交风控服务失败时写入（BizTypeAsyncAudit），由后续补偿 job
// 扫描重试（阶段 5 实现），保证"提交即返回"的视频风控请求不会丢失。
// biz_id 同时是 callback_record.id（对账纽带）。
type Compensation struct {
	ID         uint64
	UserID     int64
	ProjectID  string
	BizID      string
	BizType    int
	Payload    string // 待重试的原始请求（JSON）
	Status     int
	RetryCount int
	CreateTime int64
	UpdateTime int64
}

// CompensationRepository 补偿仓储接口。
type CompensationRepository interface {
	// Insert 写一条补偿记录，返回自增 ID。
	Insert(ctx context.Context, c *Compensation) (uint64, error)
	// GetByBizID 按 biz_id + biz_type 查补偿记录（幂等：同一笔提交失败不重复补偿）。
	GetByBizID(ctx context.Context, bizID string, bizType int) (*Compensation, error)
	// GetPending 取待重试的补偿记录（status=0），按 id 升序，最多 limit 条（补偿 job 用）。
	GetPending(ctx context.Context, limit int) ([]*Compensation, error)
	// MarkDone 标记补偿记录为已处理（重试成功或超上限放弃）。
	MarkDone(ctx context.Context, id uint64) error
	// IncrRetryCount 重试次数 +1（重试失败时调用）。
	IncrRetryCount(ctx context.Context, id uint64) error
}

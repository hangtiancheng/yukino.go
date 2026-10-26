package domain

import "context"

// ProjectRepository 作品仓储接口。
type ProjectRepository interface {
	// GetBySessionID 按 session + user 查作品（幂等检查用）。
	GetBySessionID(ctx context.Context, sessionID string, userID int64) ([]*Project, error)
	// GetByProjectID 按 project_id 查作品。
	GetByProjectID(ctx context.Context, projectID string) (*Project, error)
	// Get 按主键查作品。
	Get(ctx context.Context, id uint64) (*Project, error)
	// Insert 插入作品。
	Insert(ctx context.Context, p *Project) error
	// Update 全字段持久化（聚合根整体保存）。
	// ⚠️ 注意: points_status=0 (PointStatusUnUsed) 是全字段更新, 实现需显式列出列（避免零值字段丢失）。
	Update(ctx context.Context, p *Project) error
	// UpdateIfStatus 乐观锁全字段持久化：仅当 DB 中 status 属于 expectedStatuses 之一才写入。
	// 返回是否更新成功（RowsAffected==1）。用于状态机流转的 CAS——
	// 并发下后写者因状态已被对方推进而 CAS 失败（false），避免覆盖对方刚写入的终态。
	UpdateIfStatus(ctx context.Context, p *Project, expectedStatuses ...string) (bool, error)
	// GetStuckProjects 查超时未完成的作品（超时兜底 job 用）。
	GetStuckProjects(ctx context.Context, deadline int64) ([]*Project, error)
}

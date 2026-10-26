package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

	"gorm.io/gorm"
)

// projectRow 持久化结构（gorm 映射 cg_creation_project）。
// 域模型 Project 为纯结构体（无 gorm tag），gorm tag 仅存在于本私有行结构。
type projectRow struct {
	ID               uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	UserID           int64  `gorm:"column:user_id;not null;index:idx_session"`
	SessionID        string `gorm:"column:session_id;size:64;not null;index:idx_session"`
	ProjectID        string `gorm:"column:project_id;size:64;not null;index:idx_project_id"`
	ProjectType      string `gorm:"column:project_type;size:32;not null"`
	ProjectInput     string `gorm:"column:project_input;type:text"`
	Status           string `gorm:"column:status;size:16;not null;default:running"`
	PointsStatus     int    `gorm:"column:points_status;not null;default:0"`
	PointsCost       int    `gorm:"column:points_cost;default:0"`
	PointsOrderID    string `gorm:"column:points_order_id;size:128;default:''"`
	RetryNum         int    `gorm:"column:retry_num;default:0"`
	FailReason       string `gorm:"column:fail_reason;type:text"`
	Ext              string `gorm:"column:ext;type:text"` // JSON: TaskStep 映射、MsgID 等
	ProductionLineID string `gorm:"column:production_line_id;size:128;default:''"`
	Progress         int    `gorm:"column:progress;default:0"`
	Title            string `gorm:"column:title;size:256;default:''"`
	CreateTime       int64  `gorm:"column:create_time;not null"`
	UpdateTime       int64  `gorm:"column:update_time;not null"`
}

// TableName 指定表名。
func (projectRow) TableName() string { return "cg_creation_project" }

// Migrate 对本 context 的表执行 AutoMigrate（database.Init 启动时调用）。
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&projectRow{}, &callbackRecordRow{}, &compensationRow{})
}

// ProjectRepositoryImpl 基于 gorm 的 ProjectRepository 实现。
type ProjectRepositoryImpl struct {
	db *gorm.DB
}

// NewProjectRepositoryImpl 创建实现。
func NewProjectRepositoryImpl(db *gorm.DB) *ProjectRepositoryImpl {
	return &ProjectRepositoryImpl{db: db}
}

// 编译期断言：确保实现满足 domain.ProjectRepository 接口。
var _ domain.ProjectRepository = (*ProjectRepositoryImpl)(nil)

// toProjectRow 将域实体转为持久化结构。
func toProjectRow(f *domain.Project) *projectRow {
	return &projectRow{
		ID:               f.ID,
		UserID:           f.UserID,
		SessionID:        f.SessionID,
		ProjectID:        f.ProjectID,
		ProjectType:      f.ProjectType,
		ProjectInput:     f.ProjectInput,
		Status:           f.Status,
		PointsStatus:     f.PointsStatus,
		PointsCost:       f.PointsCost,
		PointsOrderID:    f.PointsOrderID,
		RetryNum:         f.RetryNum,
		FailReason:       f.FailReason,
		Ext:              f.Ext,
		ProductionLineID: f.ProductionLineID,
		Progress:         f.Progress,
		Title:            f.Title,
		CreateTime:       f.CreateTime,
		UpdateTime:       f.UpdateTime,
	}
}

// toProjectDomain 将持久化结构转为域实体。
func toProjectDomain(r *projectRow) *domain.Project {
	return &domain.Project{
		ID:               r.ID,
		UserID:           r.UserID,
		SessionID:        r.SessionID,
		ProjectID:        r.ProjectID,
		ProjectType:      r.ProjectType,
		ProjectInput:     r.ProjectInput,
		Status:           r.Status,
		PointsStatus:     r.PointsStatus,
		PointsCost:       r.PointsCost,
		PointsOrderID:    r.PointsOrderID,
		RetryNum:         r.RetryNum,
		FailReason:       r.FailReason,
		Ext:              r.Ext,
		ProductionLineID: r.ProductionLineID,
		Progress:         r.Progress,
		Title:            r.Title,
		CreateTime:       r.CreateTime,
		UpdateTime:       r.UpdateTime,
	}
}

// GetBySessionID 按 session + user 查作品（幂等判断的核心）。
func (r *ProjectRepositoryImpl) GetBySessionID(ctx context.Context, sessionID string, userID int64) ([]*domain.Project, error) {
	var rows []*projectRow
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND user_id = ?", sessionID, userID).
		Order("id DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	list := make([]*domain.Project, 0, len(rows))
	for _, row := range rows {
		list = append(list, toProjectDomain(row))
	}
	return list, nil
}

// GetByProjectID 按 project_id 查作品，未找到返回 domain.ErrProjectNotFound。
func (r *ProjectRepositoryImpl) GetByProjectID(ctx context.Context, projectID string) (*domain.Project, error) {
	var row projectRow
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return toProjectDomain(&row), nil
}

// Get 按主键查作品，未找到返回 domain.ErrProjectNotFound。
func (r *ProjectRepositoryImpl) Get(ctx context.Context, id uint64) (*domain.Project, error) {
	var row projectRow
	err := r.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return toProjectDomain(&row), nil
}

// Insert 插入作品。create_time/update_time 缺省填当前时间，并回填自增 ID。
func (r *ProjectRepositoryImpl) Insert(ctx context.Context, p *domain.Project) error {
	now := time.Now().Unix()
	row := toProjectRow(p)
	if row.CreateTime == 0 {
		row.CreateTime = now
	}
	if row.UpdateTime == 0 {
		row.UpdateTime = now
	}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	// 回填自增 ID
	p.ID = row.ID
	return nil
}

// Update 全字段持久化（聚合根整体保存）。
//
// ⚠️ 零值陷阱：points_status=0 (PointStatusUnUsed) 是合法取值，struct 方式
// Updates 会跳过零值字段导致丢失。因此显式 Select 列 + map 更新，确保
// points_status=0 也被写库。update_time 缺省刷新为当前时间。
func (r *ProjectRepositoryImpl) Update(ctx context.Context, p *domain.Project) error {
	_, err := r.updateWithStatus(ctx, p)
	return err
}

// UpdateIfStatus 乐观锁全字段持久化：仅当 DB 中 status 属于 expectedStatuses 之一才写入。
//
// 状态机流转的 CAS（防并发覆盖）：handler 的 running→audit 与 risk 回调的 audit→done/fail
// 各持一份 project 快照，若都用无条件的全字段 Update，后写者会把前写者刚落定的终态覆盖掉
// （audit→done 丢失更新竞态）。本方法用 `WHERE id=? AND status IN (...)` 保证只有 DB 状态仍
// 匹配才写入，CAS 失败（false）表示状态已被对方推进，调用方应幂等短路而非覆盖。
// 支持多期望状态：落定终态时 project 可能处于 audit 或 running（旧协议无 record 对账场景）。
func (r *ProjectRepositoryImpl) UpdateIfStatus(ctx context.Context, p *domain.Project, expectedStatuses ...string) (bool, error) {
	return r.updateWithStatus(ctx, p, expectedStatuses...)
}

// updateWithStatus 全字段持久化的公共实现。expectedStatuses 为空时无条件更新（等价 Update），
// 非空时附加 WHERE status IN (...) 做 CAS，返回是否命中（RowsAffected==1）。
func (r *ProjectRepositoryImpl) updateWithStatus(ctx context.Context, p *domain.Project, expectedStatuses ...string) (bool, error) {
	updateTime := p.UpdateTime
	if updateTime == 0 {
		updateTime = time.Now().Unix()
	}
	row := toProjectRow(p)
	query := r.db.WithContext(ctx).Model(&projectRow{}).Where("id = ?", p.ID)
	if len(expectedStatuses) > 0 {
		query = query.Where("status IN ?", expectedStatuses)
	}
	res := query.Select("status", "points_status", "points_cost", "points_order_id", "retry_num", "fail_reason", "ext", "production_line_id", "progress", "title", "update_time").
		Updates(map[string]any{
			"status":             row.Status,
			"points_status":      row.PointsStatus,
			"points_cost":        row.PointsCost,
			"points_order_id":    row.PointsOrderID,
			"retry_num":          row.RetryNum,
			"fail_reason":        row.FailReason,
			"ext":                row.Ext,
			"production_line_id": row.ProductionLineID,
			"progress":           row.Progress,
			"title":              row.Title,
			"update_time":        updateTime,
		})
	return res.RowsAffected == 1, res.Error
}

// GetStuckProjects 查创建时间早于 deadline 且仍处于 running 的项目（超时兜底 job 用）。
func (r *ProjectRepositoryImpl) GetStuckProjects(ctx context.Context, deadline int64) ([]*domain.Project, error) {
	var rows []*projectRow
	err := r.db.WithContext(ctx).
		Where("status = ? AND create_time < ?", domain.ProjectStatusRunning, deadline).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	list := make([]*domain.Project, 0, len(rows))
	for _, row := range rows {
		list = append(list, toProjectDomain(row))
	}
	return list, nil
}

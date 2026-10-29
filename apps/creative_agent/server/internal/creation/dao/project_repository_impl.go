package dao

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/creation/domain"

	"gorm.io/gorm"
)

// projectRow is the persistence structure (gorm mapping cg_creation_project).
// The domain model Project is a plain struct (no gorm tags); gorm tags live only in this private row struct.
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
	Ext              string `gorm:"column:ext;type:text"` // JSON: TaskStep mapping, MsgID, etc.
	ProductionLineID string `gorm:"column:production_line_id;size:128;default:''"`
	Progress         int    `gorm:"column:progress;default:0"`
	Title            string `gorm:"column:title;size:256;default:''"`
	CreateTime       int64  `gorm:"column:create_time;not null"`
	UpdateTime       int64  `gorm:"column:update_time;not null"`
}

// TableName specifies the table name.
func (projectRow) TableName() string { return "cg_creation_project" }

// Migrate runs AutoMigrate for this context's tables (called at database.Init startup).
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&projectRow{}, &callbackRecordRow{}, &compensationRow{})
}

// ProjectRepositoryImpl is the gorm-based ProjectRepository implementation.
type ProjectRepositoryImpl struct {
	db *gorm.DB
}

// NewProjectRepositoryImpl creates the implementation.
func NewProjectRepositoryImpl(db *gorm.DB) *ProjectRepositoryImpl {
	return &ProjectRepositoryImpl{db: db}
}

// Compile-time assertion: the implementation satisfies the domain.ProjectRepository interface.
var _ domain.ProjectRepository = (*ProjectRepositoryImpl)(nil)

// toProjectRow converts the domain entity into the persistence structure.
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

// toProjectDomain converts the persistence structure into the domain entity.
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

// GetBySessionID queries the work by session + user (the core of idempotency checks).
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

// GetByProjectID queries the work by project_id; returns domain.ErrProjectNotFound when absent.
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

// Get queries the work by primary key; returns domain.ErrProjectNotFound when absent.
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

// Insert inserts a work. create_time/update_time default to now, and the auto-increment ID is backfilled.
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
	// Backfill the auto-increment ID
	p.ID = row.ID
	return nil
}

// Update persists all fields (saving the aggregate root as a whole).
//
// ⚠️ Zero-value pitfall: points_status=0 (PointStatusUnUsed) is a valid value, but struct-based
// Updates skip zero-value fields and lose it. So explicit Select columns + map update ensure
// points_status=0 is also written. update_time defaults to refreshing to now.
func (r *ProjectRepositoryImpl) Update(ctx context.Context, p *domain.Project) error {
	_, err := r.updateWithStatus(ctx, p)
	return err
}

// UpdateIfStatus is optimistic-lock full-field persistence: it writes only when the DB status is one of expectedStatuses.
//
// The CAS of state-machine transitions (preventing concurrent overwrites): the handler's running→audit and the risk callback's audit→done/fail
// each hold a project snapshot; if both used unconditional full-field Updates, the later writer would overwrite the terminal state the earlier writer just settled
// (the audit→done lost-update race). This method uses `WHERE id=? AND status IN (...)` so it writes only while the DB status
// still matches; a CAS failure (false) means the state already advanced, and callers should short-circuit idempotently instead of overwriting.
// Multiple expected statuses are supported: when settling terminal, the project may be audit or running (legacy protocol without record reconciliation).
func (r *ProjectRepositoryImpl) UpdateIfStatus(ctx context.Context, p *domain.Project, expectedStatuses ...string) (bool, error) {
	return r.updateWithStatus(ctx, p, expectedStatuses...)
}

// updateWithStatus is the shared full-field persistence implementation. With empty expectedStatuses it updates unconditionally (equivalent to Update);
// otherwise it appends WHERE status IN (...) as the CAS and returns whether it hit (RowsAffected==1).
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

// GetStuckProjects queries projects created before deadline that are still running (for the timeout-fallback job).
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

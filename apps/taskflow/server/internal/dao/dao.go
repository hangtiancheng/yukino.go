package dao

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/components/consistent_cache"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DAO struct {
	db *gorm.DB
}

func New(db *gorm.DB) *DAO {
	return &DAO{db: db}
}

func (d *DAO) DB() *gorm.DB { return d.db }

func (d *DAO) InsertPending(ctx context.Context, exec *po.Execution) (created bool, id uint, err error) {
	result := d.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "fire_key"}},
		DoNothing: true,
	}).Create(exec)
	if result.Error != nil {
		return false, 0, result.Error
	}
	var existing po.Execution
	if err := d.db.WithContext(ctx).Where("fire_key = ?", exec.FireKey).First(&existing).Error; err != nil {
		return false, 0, err
	}
	exec.ID = existing.ID
	return result.RowsAffected == 1, existing.ID, nil
}

func (d *DAO) GetExecution(ctx context.Context, id uint) (*po.Execution, error) {
	var exec po.Execution
	err := d.db.WithContext(ctx).First(&exec, id).Error
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (d *DAO) GetExecutionByFireKey(ctx context.Context, fireKey string) (*po.Execution, error) {
	var exec po.Execution
	err := d.db.WithContext(ctx).Where("fire_key = ?", fireKey).First(&exec).Error
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (d *DAO) Transition(ctx context.Context, id uint, from []string, to string, updates map[string]any) (int64, error) {
	values := make(map[string]any, len(updates)+1)
	for key, value := range updates {
		values[key] = value
	}
	values["status"] = to
	tx := d.db.WithContext(ctx).Model(&po.Execution{}).Where("id = ? AND status IN ?", id, from).Updates(values)
	return tx.RowsAffected, tx.Error
}

func (d *DAO) TransitionOwned(ctx context.Context, id uint, txID string, from []string, to string, updates map[string]any) (int64, error) {
	values := make(map[string]any, len(updates)+1)
	for key, value := range updates {
		values[key] = value
	}
	values["status"] = to
	result := d.db.WithContext(ctx).Model(&po.Execution{}).Where("id = ? AND tx_id = ? AND status IN ?", id, txID, from).Updates(values)
	return result.RowsAffected, result.Error
}

type ExecutionFilter struct {
	TaskType string
	TaskID   uint
	Status   string
	Page     int
	PageSize int
}

func (d *DAO) ListExecutions(ctx context.Context, f ExecutionFilter) ([]*po.Execution, int64, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.PageSize <= 0 || f.PageSize > 200 {
		f.PageSize = 20
	}

	q := d.db.WithContext(ctx).Model(&po.Execution{})
	if f.TaskType != "" {
		q = q.Where("task_type = ?", f.TaskType)
	}
	if f.TaskID != 0 {
		q = q.Where("task_id = ?", f.TaskID)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []*po.Execution
	err := q.Order("id DESC").Offset((f.Page - 1) * f.PageSize).Limit(f.PageSize).Find(&rows).Error
	return rows, total, err
}

func (d *DAO) ExecutionStats(ctx context.Context, since time.Time) (map[string]int64, error) {
	type row struct {
		Status string
		Cnt    int64
	}
	var rows []row
	err := d.db.WithContext(ctx).Model(&po.Execution{}).
		Select("status, count(*) as cnt").
		Where("created_at >= ?", since).
		Group("status").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	stats := make(map[string]int64, len(rows))
	for _, r := range rows {
		stats[r.Status] = r.Cnt
	}
	return stats, nil
}

func (d *DAO) StuckExecutions(ctx context.Context, runningBefore time.Time) ([]*po.Execution, error) {
	var rows []*po.Execution
	err := d.db.WithContext(ctx).
		Where("status = ? AND updated_at < ?", po.StatusRunning, runningBefore).
		Order("id ASC").Limit(200).Find(&rows).Error
	return rows, err
}

func (d *DAO) QueuedStuck(ctx context.Context, before time.Time) ([]*po.Execution, error) {
	var rows []*po.Execution
	err := d.db.WithContext(ctx).
		Where("status = ? AND updated_at < ?", po.StatusQueued, before).
		Order("id ASC").Limit(200).Find(&rows).Error
	return rows, err
}

func (d *DAO) PendingOverdue(ctx context.Context, before time.Time) ([]*po.Execution, error) {
	var rows []*po.Execution
	err := d.db.WithContext(ctx).
		Where("status = ? AND fire_at < ?", po.StatusPending, before).
		Order("id ASC").Limit(200).Find(&rows).Error
	return rows, err
}

func (d *DAO) RecentlyUpdated(ctx context.Context, since time.Time, limit int) ([]*po.Execution, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	var rows []*po.Execution
	err := d.db.WithContext(ctx).
		Select("id", "status", "fire_key", "task_type", "updated_at").
		Where("updated_at >= ?", since).
		Order("updated_at DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (d *DAO) ListScheduledTasks(ctx context.Context) ([]*po.ScheduledTask, error) {
	var rows []*po.ScheduledTask
	err := d.db.WithContext(ctx).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (d *DAO) EnabledScheduledTasks(ctx context.Context) ([]*po.ScheduledTask, error) {
	var rows []*po.ScheduledTask
	err := d.db.WithContext(ctx).Where("enabled = ?", true).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (d *DAO) GetScheduledTask(ctx context.Context, id uint) (*po.ScheduledTask, error) {
	var row po.ScheduledTask
	if err := d.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (d *DAO) CreateScheduledTask(ctx context.Context, row *po.ScheduledTask) error {
	return d.db.WithContext(ctx).Create(row).Error
}

func (d *DAO) UpdateScheduledTask(ctx context.Context, row *po.ScheduledTask) error {
	return d.db.WithContext(ctx).Model(&po.ScheduledTask{}).
		Where("id = ?", row.ID).
		Updates(map[string]any{
			"name": row.Name, "description": row.Description, "cron_expr": row.CronExpr,
			"timezone": row.Timezone, "prompt": row.Prompt, "model": row.Model,
			"enabled": row.Enabled, "next_fire_at": row.NextFireAt,
		}).Error
}

func (d *DAO) DeleteScheduledTask(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Delete(&po.ScheduledTask{}, id).Error
}

func (d *DAO) ListConditionTasks(ctx context.Context) ([]*po.ConditionTask, error) {
	var rows []*po.ConditionTask
	err := d.db.WithContext(ctx).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (d *DAO) EnabledConditionTasks(ctx context.Context, tableName string) ([]*po.ConditionTask, error) {
	var rows []*po.ConditionTask
	q := d.db.WithContext(ctx).Where("enabled = ? AND event_type = ?", true, po.EventTypeMySQLInsert)
	if tableName != "" {
		q = q.Where("table_name = ?", tableName)
	}
	err := q.Order("id ASC").Find(&rows).Error
	return rows, err
}

func (d *DAO) GetConditionTask(ctx context.Context, id uint) (*po.ConditionTask, error) {
	var row po.ConditionTask
	if err := d.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (d *DAO) CreateConditionTask(ctx context.Context, row *po.ConditionTask) error {
	return d.db.WithContext(ctx).Create(row).Error
}

func (d *DAO) UpdateConditionTask(ctx context.Context, row *po.ConditionTask) error {
	return d.db.WithContext(ctx).Model(&po.ConditionTask{}).
		Where("id = ?", row.ID).
		Updates(map[string]any{
			"name": row.Name, "description": row.Description, "table_name": row.WatchTable,
			"prompt": row.Prompt, "model": row.Model, "enabled": row.Enabled,
		}).Error
}

func (d *DAO) DeleteConditionTask(ctx context.Context, id uint) error {
	return d.db.WithContext(ctx).Delete(&po.ConditionTask{}, id).Error
}

func (d *DAO) CreateRiskRecord(ctx context.Context, row *po.RiskRecord) error {
	return d.db.WithContext(ctx).Create(row).Error
}

func (d *DAO) ListRiskRecords(ctx context.Context, page, pageSize int) ([]*po.RiskRecord, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	var total int64
	if err := d.db.WithContext(ctx).Model(&po.RiskRecord{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*po.RiskRecord
	err := d.db.WithContext(ctx).Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

func (d *DAO) GetRiskRecord(ctx context.Context, id uint) (*po.RiskRecord, error) {
	var row po.RiskRecord
	if err := d.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (d *DAO) CreateDeadLetter(ctx context.Context, row *po.DeadLetter) error {
	return d.db.WithContext(ctx).Create(row).Error
}

func (d *DAO) ListDeadLetters(ctx context.Context, page, pageSize int) ([]*po.DeadLetter, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	var total int64
	if err := d.db.WithContext(ctx).Model(&po.DeadLetter{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []*po.DeadLetter
	err := d.db.WithContext(ctx).Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error
	return rows, total, err
}

type ScheduledTaskObject struct {
	po.ScheduledTask
}

func NewScheduledTaskObject(id uint) *ScheduledTaskObject {
	return &ScheduledTaskObject{po.ScheduledTask{ID: id}}
}

func (o *ScheduledTaskObject) TableName() string { return "scheduled_tasks" }
func (o *ScheduledTaskObject) KeyColumn() string { return "id" }
func (o *ScheduledTaskObject) Key() string       { return strconv.FormatUint(uint64(o.ID), 10) }

func (o *ScheduledTaskObject) Write() (string, error) {
	body, err := json.Marshal(o.ScheduledTask)
	return string(body), err
}

func (o *ScheduledTaskObject) Read(body string) error {
	return json.Unmarshal([]byte(body), &o.ScheduledTask)
}

type ConditionTaskObject struct {
	po.ConditionTask
}

func NewConditionTaskObject(id uint) *ConditionTaskObject {
	return &ConditionTaskObject{po.ConditionTask{ID: id}}
}

func (o *ConditionTaskObject) TableName() string { return "condition_tasks" }
func (o *ConditionTaskObject) KeyColumn() string { return "id" }
func (o *ConditionTaskObject) Key() string       { return strconv.FormatUint(uint64(o.ID), 10) }

func (o *ConditionTaskObject) Write() (string, error) {
	body, err := json.Marshal(o.ConditionTask)
	return string(body), err
}

func (o *ConditionTaskObject) Read(body string) error {
	return json.Unmarshal([]byte(body), &o.ConditionTask)
}

var ErrDefNotExist = consistent_cache.ErrorDataNotExist

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, consistent_cache.ErrorDataNotExist)
}

func IsDuplicateEntry(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "1062")
}

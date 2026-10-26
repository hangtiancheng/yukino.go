package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrAsyncRequestNotFound 表示未找到对应异步请求记录。
var ErrAsyncRequestNotFound = errors.New("async request not found")

// ErrAsyncRequestConflict 表示 CAS 抢占冲突（已被其他 worker 抢占）。
var ErrAsyncRequestConflict = errors.New("async request conflict: already taken by another worker")

// AsyncRequestRepository 封装 cg_async_request 表的数据访问。
//
// 核心语义：
//   - InsertPending: 可靠性第一道——先落库再投递
//   - Takeover: CAS 抢占（WHERE id=? AND status=Pending），防多 Worker 重复执行
//   - ListZombies: 僵尸扫描（InProgress + heartbeat_at 过期）
//   - ResetForRetry: 策略B 重投前复位 InProgress→Pending
//   - UpdateHeartbeat: 心跳刷新，Worker 每 5s 调用
type AsyncRequestRepository interface {
	// InsertPending 插入一条 Pending 状态的异步请求记录。
	InsertPending(ctx context.Context, req *model.CGAsyncRequest) error

	// Takeover 原子地将状态从 Pending 翻为 InProgress 并设置 owner。
	// CAS 语义：WHERE async_request_id=? AND status=Pending，RowsAffected==0 表示已被抢占。
	// asyncRequestID 是雪花业务键（stream 载荷携带的值），不是自增主键 id。
	Takeover(ctx context.Context, asyncRequestID string, workerID string) (*model.CGAsyncRequest, error)

	// MarkTerminal 将状态标记为终态（Completed 或 Failed）。
	MarkTerminal(ctx context.Context, asyncRequestID int64, status int8) error

	// ListZombies 查找 InProgress 且 heartbeat_at 早于 heartbeatExpiry 的记录。
	ListZombies(ctx context.Context, heartbeatExpiry time.Time) ([]*model.CGAsyncRequest, error)

	// UpdateHeartbeat 刷新 heartbeat_at 为当前时间（Worker 心跳）。
	UpdateHeartbeat(ctx context.Context, asyncRequestID int64) error

	// ResetForRetry 将 InProgress 记录打回 Pending，清空 owner。
	// 僵尸恢复策略B 使用：重投前复位，避免 recovery 每轮重复扫到同一条。
	ResetForRetry(ctx context.Context, asyncRequestID int64) error

	// GetByMessageID 按 message_id 查询记录（RunAttach 用来检查终态）。
	GetByMessageID(ctx context.Context, messageID string) (*model.CGAsyncRequest, error)

	// GetByID 按自增主键查询记录。
	GetByID(ctx context.Context, id int64) (*model.CGAsyncRequest, error)
}

type asyncRequestRepository struct {
	db *gorm.DB
}

// NewAsyncRequestRepository 基于 gorm.DB 创建 AsyncRequestRepository。
func NewAsyncRequestRepository(db *gorm.DB) AsyncRequestRepository {
	return &asyncRequestRepository{db: db}
}

func (r *asyncRequestRepository) InsertPending(ctx context.Context, req *model.CGAsyncRequest) error {
	now := time.Now().UnixMilli()
	if req.CreateTime == 0 {
		req.CreateTime = now
	}
	if req.UpdateTime == 0 {
		req.UpdateTime = now
	}
	if req.AsyncRequestID == "" {
		req.AsyncRequestID = model.NextIDString()
	}
	req.Status = model.AsyncStatusPending
	return r.db.WithContext(ctx).Create(req).Error
}

// Takeover 原子地将状态从 Pending 翻为 InProgress 并设置 owner。
// 使用 CAS 语义：WHERE async_request_id=? AND status=Pending，失败表示已被抢占。
func (r *asyncRequestRepository) Takeover(ctx context.Context, asyncRequestID string, workerID string) (*model.CGAsyncRequest, error) {
	now := time.Now().UnixMilli()
	res := r.db.WithContext(ctx).
		Model(&model.CGAsyncRequest{}).
		Where("async_request_id = ? AND status = ?", asyncRequestID, model.AsyncStatusPending).
		Updates(map[string]any{
			"status":        model.AsyncStatusInProgress,
			"worker_id":     workerID,
			"heartbeat_at":  now,
			"attempt_count": gorm.Expr("attempt_count + 1"),
			"update_time":   now,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrAsyncRequestConflict
	}

	// Re-fetch to get the updated record
	return r.GetByAsyncRequestID(ctx, asyncRequestID)
}

func (r *asyncRequestRepository) MarkTerminal(ctx context.Context, asyncRequestID int64, status int8) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).
		Model(&model.CGAsyncRequest{}).
		Where("id = ?", asyncRequestID).
		Updates(map[string]any{
			"status":      status,
			"update_time": now,
		}).Error
}

// ListZombies 查找 InProgress 且心跳在 expiry 之前的记录。
func (r *asyncRequestRepository) ListZombies(ctx context.Context, heartbeatExpiry time.Time) ([]*model.CGAsyncRequest, error) {
	var records []*model.CGAsyncRequest
	expiryMs := heartbeatExpiry.UnixMilli()
	err := r.db.WithContext(ctx).
		Where("status = ? AND heartbeat_at < ?", model.AsyncStatusInProgress, expiryMs).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (r *asyncRequestRepository) UpdateHeartbeat(ctx context.Context, asyncRequestID int64) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).
		Model(&model.CGAsyncRequest{}).
		Where("id = ?", asyncRequestID).
		Updates(map[string]any{
			"heartbeat_at": now,
			"update_time":  now,
		}).Error
}

// ResetForRetry 将状态从 InProgress 打回 Pending 并清空 owner。
// 用于僵尸恢复策略B：重投前先复位，避免 recovery 每轮重复扫到同一条。
func (r *asyncRequestRepository) ResetForRetry(ctx context.Context, asyncRequestID int64) error {
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).
		Model(&model.CGAsyncRequest{}).
		Where("id = ?", asyncRequestID).
		Updates(map[string]any{
			"status":      model.AsyncStatusPending,
			"worker_id":   "",
			"update_time": now,
		}).Error
}

func (r *asyncRequestRepository) GetByMessageID(ctx context.Context, messageID string) (*model.CGAsyncRequest, error) {
	var req model.CGAsyncRequest
	err := r.db.WithContext(ctx).Where("message_id = ?", messageID).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAsyncRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// GetByAsyncRequestID 按雪花业务键查询记录。
func (r *asyncRequestRepository) GetByAsyncRequestID(ctx context.Context, asyncRequestID string) (*model.CGAsyncRequest, error) {
	var req model.CGAsyncRequest
	err := r.db.WithContext(ctx).Where("async_request_id = ?", asyncRequestID).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAsyncRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *asyncRequestRepository) GetByID(ctx context.Context, id int64) (*model.CGAsyncRequest, error) {
	var req model.CGAsyncRequest
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&req).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAsyncRequestNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrAsyncRequestNotFound indicates no matching async-request record was found.
var ErrAsyncRequestNotFound = errors.New("async request not found")

// ErrAsyncRequestConflict indicates a CAS claim conflict (already claimed by another worker).
var ErrAsyncRequestConflict = errors.New("async request conflict: already taken by another worker")

// AsyncRequestRepository encapsulates data access to the cg_async_request table.
//
// Core semantics:
//   - InsertPending: the first reliability guard — persist before dispatching
//   - Takeover: CAS claim (WHERE id=? AND status=Pending), preventing duplicate execution by multiple Workers
//   - ListZombies: zombie scan (InProgress + expired heartbeat_at)
//   - ResetForRetry: strategy B resets InProgress→Pending before redispatch
//   - UpdateHeartbeat: heartbeat refresh, called by the Worker every 5s
type AsyncRequestRepository interface {
	// InsertPending inserts one async-request record in Pending status.
	InsertPending(ctx context.Context, req *model.CGAsyncRequest) error

	// Takeover atomically flips the status from Pending to InProgress and sets the owner.
	// CAS semantics: WHERE async_request_id=? AND status=Pending; RowsAffected==0 means it was already claimed.
	// asyncRequestID is the snowflake business key (the value carried in the stream payload), not the auto-increment primary key id.
	Takeover(ctx context.Context, asyncRequestID string, workerID string) (*model.CGAsyncRequest, error)

	// MarkTerminal marks the status terminal (Completed or Failed).
	MarkTerminal(ctx context.Context, asyncRequestID int64, status int8) error

	// ListZombies finds InProgress records whose heartbeat_at is earlier than heartbeatExpiry.
	ListZombies(ctx context.Context, heartbeatExpiry time.Time) ([]*model.CGAsyncRequest, error)

	// UpdateHeartbeat refreshes heartbeat_at to now (Worker heartbeat).
	UpdateHeartbeat(ctx context.Context, asyncRequestID int64) error

	// ResetForRetry resets an InProgress record back to Pending and clears the owner.
	// Used by zombie-recovery strategy B: reset before redispatch so recovery does not rescan the same record every round.
	ResetForRetry(ctx context.Context, asyncRequestID int64) error

	// GetByMessageID queries a record by message_id (RunAttach uses it to check the terminal state).
	GetByMessageID(ctx context.Context, messageID string) (*model.CGAsyncRequest, error)

	// GetByID queries a record by auto-increment primary key.
	GetByID(ctx context.Context, id int64) (*model.CGAsyncRequest, error)
}

type asyncRequestRepository struct {
	db *gorm.DB
}

// NewAsyncRequestRepository creates an AsyncRequestRepository on top of gorm.DB.
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

// Takeover atomically flips the status from Pending to InProgress and sets the owner.
// Uses CAS semantics: WHERE async_request_id=? AND status=Pending; failure means it was already claimed.
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

// ListZombies finds InProgress records whose heartbeat is earlier than expiry.
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

// ResetForRetry resets the status from InProgress back to Pending and clears the owner.
// Used by zombie-recovery strategy B: reset before redispatch so recovery does not rescan the same record every round.
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

// GetByAsyncRequestID queries a record by its snowflake business key.
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

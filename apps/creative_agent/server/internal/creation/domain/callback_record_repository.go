package domain

import (
	"context"
	"errors"
)

// ErrCallbackRecordNotFound indicates no matching callback record was found.
var ErrCallbackRecordNotFound = errors.New("callback record not found")

// CallbackRecordRepository is the task callback record repository interface.
type CallbackRecordRepository interface {
	// Insert inserts a callback record and returns the auto-increment ID.
	Insert(ctx context.Context, r *CallbackRecord) (uint64, error)
	// Get queries a callback record by primary key.
	Get(ctx context.Context, id uint64) (*CallbackRecord, error)
	// GetByProjectAndType queries a callback record by project + task_type (for idempotency / reconciliation).
	GetByProjectAndType(ctx context.Context, projectID, taskType string) (*CallbackRecord, error)
	// Update updates the given fields of a callback record by primary key (non-terminal fields only).
	Update(ctx context.Context, id uint64, assigns map[string]any) error
	// MarkStatusIf optimistic lock: sets the new status only when the current status is within allowedFrom.
	// Returns whether the update succeeded (rows affected = 1).
	//
	// Caller contract: state transitions must pass allowedFrom explicitly (callback-replay idempotency depends on it);
	// with an empty allowedFrom implementations update unconditionally (no optimistic-lock protection), for non-terminal transitions only.
	MarkStatusIf(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error)
}

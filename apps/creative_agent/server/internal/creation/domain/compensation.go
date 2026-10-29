package domain

import (
	"context"
	"errors"
)

// Compensation business types (the stage-5 compensation job retries by these categories).
const (
	BizTypeAsyncAudit    = 1 // async risk submission failure
	BizTypeCallbackAgent = 2 // callback-agent failure
)

// Compensation record statuses (cg_audit_compensation.status).
const (
	CompensationStatusPending = 0 // pending (the job scans and retries)
	CompensationStatusDone    = 1 // processed (retry succeeded / abandoned beyond the cap)
)

// ErrCompensationNotFound indicates no matching compensation record was found.
var ErrCompensationNotFound = errors.New("compensation not found")

// Compensation is a compensation record (cg_audit_compensation).
//
// Written when AsyncAudit submission to the risk service fails (BizTypeAsyncAudit); later compensation jobs
// scan and retry it (implemented in stage 5), guaranteeing "submit-and-return" video risk requests are never lost.
// biz_id is also the callback_record.id (the reconciliation bond).
type Compensation struct {
	ID         uint64
	UserID     int64
	ProjectID  string
	BizID      string
	BizType    int
	Payload    string // the original request to retry (JSON)
	Status     int
	RetryCount int
	CreateTime int64
	UpdateTime int64
}

// CompensationRepository is the compensation repository interface.
type CompensationRepository interface {
	// Insert writes one compensation record and returns the auto-increment ID.
	Insert(ctx context.Context, c *Compensation) (uint64, error)
	// GetByBizID queries a compensation record by biz_id + biz_type (idempotent: one failed submission is not compensated twice).
	GetByBizID(ctx context.Context, bizID string, bizType int) (*Compensation, error)
	// GetPending fetches compensation records awaiting retry (status=0), ascending by id, at most limit rows (for the compensation job).
	GetPending(ctx context.Context, limit int) ([]*Compensation, error)
	// MarkDone marks a compensation record processed (retry succeeded or abandoned beyond the cap).
	MarkDone(ctx context.Context, id uint64) error
	// IncrRetryCount increments the retry count (called on retry failure).
	IncrRetryCount(ctx context.Context, id uint64) error
}

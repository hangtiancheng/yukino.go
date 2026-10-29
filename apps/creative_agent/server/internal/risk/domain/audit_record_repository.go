package domain

import (
	"context"
	"errors"
)

// ErrRecordNotFound indicates no matching audit record was found.
var ErrRecordNotFound = errors.New("audit record not found")

// AuditRecordRepository is the risk-audit record repository interface.
//
// Note: Update using map[string]any still leaks technical details and should converge in refactoring;
// but audit-record update points are scattered (task_input writes, terminal settling, callback_url),
// so the map keeps parity with the current state for now and can be refined later.
type AuditRecordRepository interface {
	// Insert inserts an audit record and returns the auto-increment ID.
	Insert(ctx context.Context, r *AuditRecord) (uint64, error)
	// Get queries by primary key.
	Get(ctx context.Context, id uint64) (*AuditRecord, error)
	// Update updates the given fields by primary key (non-terminal fields only).
	Update(ctx context.Context, id uint64, assigns map[string]any) error
	// MarkTerminal sets terminal with an optimistic lock: it updates only when status is within allowedFrom.
	// Returns whether the update succeeded (rows affected = 1).
	//
	// Caller contract: terminal transitions must pass allowedFrom explicitly (callback-replay idempotency depends on it);
	// with an empty allowedFrom implementations update unconditionally (no optimistic-lock protection), for non-terminal transitions only.
	MarkTerminal(ctx context.Context, id uint64, status string, allowedFrom ...string) (bool, error)
}

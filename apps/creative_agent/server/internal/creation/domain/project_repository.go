package domain

import "context"

// ProjectRepository is the work repository interface.
type ProjectRepository interface {
	// GetBySessionID queries the work by session + user (for idempotency checks).
	GetBySessionID(ctx context.Context, sessionID string, userID int64) ([]*Project, error)
	// GetByProjectID queries the work by project_id.
	GetByProjectID(ctx context.Context, projectID string) (*Project, error)
	// Get queries the work by primary key.
	Get(ctx context.Context, id uint64) (*Project, error)
	// Insert inserts a work.
	Insert(ctx context.Context, p *Project) error
	// Update persists all fields (saving the aggregate root as a whole).
	// ⚠️ Note: points_status=0 (PointStatusUnUsed) with full-field updates — implementations must list columns explicitly (avoiding losing zero-value fields).
	Update(ctx context.Context, p *Project) error
	// UpdateIfStatus is optimistic-lock full-field persistence: it writes only when the DB status is one of expectedStatuses.
	// Returns whether the update succeeded (RowsAffected==1). Used as the CAS of state-machine transitions —
	// under concurrency the later writer's CAS fails (false) because the state already advanced, avoiding overwriting the terminal state just written.
	UpdateIfStatus(ctx context.Context, p *Project, expectedStatuses ...string) (bool, error)
	// GetStuckProjects queries works that timed out unfinished (for the timeout-fallback job).
	GetStuckProjects(ctx context.Context, deadline int64) ([]*Project, error)
}

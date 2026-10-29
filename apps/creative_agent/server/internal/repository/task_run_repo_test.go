package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTaskRunTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGTaskRun{}))
	return db
}

// TestTaskRunRepository_GetWaitingBySession verifies querying the latest Waiting run by session + user.
// Used by callback advancement: project_id is the session_id; only Waiting status is queried to avoid cross-session/cross-user false hits.
func TestTaskRunRepository_GetWaitingBySession(t *testing.T) {
	db := newTaskRunTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	// Two rows in the same session: one Waiting, one Finished
	testx.NoError(t, repo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: 1, UserID: 1, SessionID: "s1", Status: model.TaskRunStatusWaiting,
		CreateTime: now, UpdateTime: now,
	}))
	testx.NoError(t, repo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: 2, UserID: 1, SessionID: "s1", Status: model.TaskRunStatusFinished,
		CreateTime: now, UpdateTime: now,
	}))

	run, err := repo.GetWaitingBySession(ctx, "s1", 1)
	testx.NoError(t, err)
	testx.NotNil(t, run)
	testx.Equal(t, int64(1), run.TaskRunID, "the Waiting row should be returned")
}

// TestTaskRunRepository_GetWaitingBySession_ScopedByUser verifies user_id filtering:
// a Waiting run of a different user in the same session must not match.
func TestTaskRunRepository_GetWaitingBySession_ScopedByUser(t *testing.T) {
	db := newTaskRunTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	// Under s1, user 2 has one Waiting run; user 1 has none
	testx.NoError(t, repo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: 1, UserID: 2, SessionID: "s1", Status: model.TaskRunStatusWaiting,
		CreateTime: now, UpdateTime: now,
	}))

	// user 1 finds nothing → ErrTaskRunNotFound
	run, err := repo.GetWaitingBySession(ctx, "s1", 1)
	testx.ErrorIs(t, err, repository.ErrTaskRunNotFound)
	testx.Nil(t, run)

	// user 2 finds it
	run, err = repo.GetWaitingBySession(ctx, "s1", 2)
	testx.NoError(t, err)
	testx.NotNil(t, run)
	testx.Equal(t, int64(1), run.TaskRunID)
}

// TestTaskRunRepository_GetWaitingBySession_NotFound verifies that ErrTaskRunNotFound is returned when there is no Waiting record.
func TestTaskRunRepository_GetWaitingBySession_NotFound(t *testing.T) {
	db := newTaskRunTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	run, err := repo.GetWaitingBySession(ctx, "no-such-session", 1)
	testx.ErrorIs(t, err, repository.ErrTaskRunNotFound)
	testx.Nil(t, run)
}

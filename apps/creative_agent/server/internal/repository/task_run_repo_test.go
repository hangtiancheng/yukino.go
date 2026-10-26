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

// TestTaskRunRepository_GetWaitingBySession 验证按 session + user 查最新 Waiting run。
// 回调推进定位用：project_id 即 session_id，只查 Waiting 状态，避免跨会话/跨用户误命中。
func TestTaskRunRepository_GetWaitingBySession(t *testing.T) {
	db := newTaskRunTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	// 同 session 两条：一条 Waiting、一条 Finished
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
	testx.Equal(t, int64(1), run.TaskRunID, "应返回 Waiting 那条")
}

// TestTaskRunRepository_GetWaitingBySession_ScopedByUser 验证 user_id 过滤：
// 同 session 不同 user 的 Waiting run 不应命中。
func TestTaskRunRepository_GetWaitingBySession_ScopedByUser(t *testing.T) {
	db := newTaskRunTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	// s1 下 user 2 有一条 Waiting，user 1 没有
	testx.NoError(t, repo.Insert(ctx, &model.CGTaskRun{
		TaskRunID: 1, UserID: 2, SessionID: "s1", Status: model.TaskRunStatusWaiting,
		CreateTime: now, UpdateTime: now,
	}))

	// user 1 查不到 → ErrTaskRunNotFound
	run, err := repo.GetWaitingBySession(ctx, "s1", 1)
	testx.ErrorIs(t, err, repository.ErrTaskRunNotFound)
	testx.Nil(t, run)

	// user 2 能查到
	run, err = repo.GetWaitingBySession(ctx, "s1", 2)
	testx.NoError(t, err)
	testx.NotNil(t, run)
	testx.Equal(t, int64(1), run.TaskRunID)
}

// TestTaskRunRepository_GetWaitingBySession_NotFound 验证无 Waiting 记录返回 ErrTaskRunNotFound。
func TestTaskRunRepository_GetWaitingBySession_NotFound(t *testing.T) {
	db := newTaskRunTestDB(t)
	repo := repository.NewTaskRunRepository(db)
	ctx := context.Background()

	run, err := repo.GetWaitingBySession(ctx, "no-such-session", 1)
	testx.ErrorIs(t, err, repository.ErrTaskRunNotFound)
	testx.Nil(t, run)
}

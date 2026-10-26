package repository_test

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/repository"
	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/pkg/testx"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newCronTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testx.NoError(t, err)
	testx.NoError(t, db.AutoMigrate(&model.CGCronTask{}))
	return db
}

func TestCronTaskRepo(t *testing.T) {
	ctx := context.Background()
	db := newCronTestDB(t)
	repo := repository.NewCronTaskRepository(db)

	// Insert
	task := &model.CGCronTask{
		CronTaskID:       1001,
		SessionID:        "s1",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"},{"agent_id":"gold_scriptwriter","input":"写脚本"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, repo.Insert(ctx, task))

	// GetByCronTaskID
	got, err := repo.GetByCronTaskID(ctx, 1001)
	testx.NoError(t, err)
	testx.NotNil(t, got)
	testx.Equal(t, model.CronTaskStatusRunning, got.Status)

	// GetByCronTaskID 未命中时返回 ErrCronTaskNotFound
	_, err = repo.GetByCronTaskID(ctx, 9999)
	testx.ErrorIs(t, err, repository.ErrCronTaskNotFound)

	// ListRunning
	running, err := repo.ListRunning(ctx)
	testx.NoError(t, err)
	testx.Len(t, running, 1)

	// AdvanceStepIndex（CAS 推进）
	ok, err := repo.AdvanceStepIndex(ctx, 1001, 0, 1)
	testx.NoError(t, err)
	testx.True(t, ok)
	got2, err := repo.GetByCronTaskID(ctx, 1001)
	testx.NoError(t, err)
	testx.Equal(t, int32(1), got2.CurrentStepIndex)

	// CAS 失败（index 不匹配）
	ok2, err := repo.AdvanceStepIndex(ctx, 1001, 0, 2)
	testx.NoError(t, err)
	testx.False(t, ok2)

	// MarkDone
	testx.NoError(t, repo.MarkDone(ctx, 1001))
	got3, err := repo.GetByCronTaskID(ctx, 1001)
	testx.NoError(t, err)
	testx.Equal(t, model.CronTaskStatusDone, got3.Status)

	// ListRunning 不再返回
	running2, err := repo.ListRunning(ctx)
	testx.NoError(t, err)
	testx.Len(t, running2, 0)

	// MarkFailed 持久化 reason
	runningTask := &model.CGCronTask{
		CronTaskID:       1002,
		SessionID:        "s2",
		PlanSteps:        `{"agents":[{"agent_id":"idea_king","input":"选题"}]}`,
		CurrentStepIndex: 0,
		Status:           model.CronTaskStatusRunning,
	}
	testx.NoError(t, repo.Insert(ctx, runningTask))
	testx.NoError(t, repo.MarkFailed(ctx, 1002, "plan_steps 解析失败"))
	gotFail, err := repo.GetByCronTaskID(ctx, 1002)
	testx.NoError(t, err)
	testx.Equal(t, model.CronTaskStatusFailed, gotFail.Status)
	testx.Equal(t, "plan_steps 解析失败", gotFail.Reason)
}

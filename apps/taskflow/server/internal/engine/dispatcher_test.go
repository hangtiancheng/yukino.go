package engine

import (
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
)

func TestScheduleEditsInvalidateOnlyNonMatchingFires(t *testing.T) {
	task := &po.ScheduledTask{CronExpr: "0 10 * * *", Timezone: "Asia/Shanghai"}
	fire := time.Date(2026, 10, 6, 2, 0, 0, 0, time.UTC)
	if !matchesSchedule(task, fire) {
		t.Fatal("10:00 local fire rejected")
	}
	task.CronExpr = "0 11 * * *"
	if matchesSchedule(task, fire) {
		t.Fatal("stale fire survived schedule change")
	}
	task.CronExpr = "invalid"
	if matchesSchedule(task, fire) {
		t.Fatal("invalid definition matched a fire")
	}
}

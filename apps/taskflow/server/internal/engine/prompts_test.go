package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
)

func TestDelayedScheduledWindowUsesFireTime(t *testing.T) {
	fire, _ := time.Parse(time.RFC3339, "2026-10-06T10:00:00+08:00")
	prompt := BuildUserPrompt(&po.Execution{TaskType: po.TaskTypeScheduled, FireAt: fire, TriggerInfo: `{"timezone":"Asia/Shanghai"}`})
	for _, date := range []string{"2026-10-06 02:00:00", "2026-10-05 02:00:00", "2026-10-04 02:00:00"} {
		if !strings.Contains(prompt, date) {
			t.Fatalf("missing window boundary %s", date)
		}
	}
}

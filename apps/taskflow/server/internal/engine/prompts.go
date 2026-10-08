package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
)

// BuildUserPrompt renders the per-execution user prompt sent to the model,
// alongside the definition prompt which travels as the system prompt.
func BuildUserPrompt(exec *po.Execution) string {
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)

	switch exec.TaskType {
	case po.TaskTypeCondition:
		b.WriteString(fmt.Sprintf("Event type: MySQL INSERT (condition task).\nCurrent time: %s\nEvent time: %s\n\n",
			now, exec.FireAt.Format(time.RFC3339)))
		b.WriteString("The record that triggered this event:\n\n```json\n")
		b.WriteString(exec.TriggerInfo)
		b.WriteString("\n```\n\n")
		b.WriteString("Follow the system prompt to finish the analysis. You may call tools to verify the real ")
		b.WriteString("database state, but your conclusion must focus on this inserted record. Output a structured markdown report.")
	case po.TaskTypeManual:
		b.WriteString(fmt.Sprintf("Trigger: manual run.\nCurrent time: %s\nRelated scheduled task ID: %d\n\n", now, exec.TaskID))
		writeAuditWindows(&b, exec)
		b.WriteString("Follow the system prompt to run the task right away and output a structured markdown report.")
	default:
		writeAuditWindows(&b, exec)
		b.WriteString(fmt.Sprintf("Trigger: scheduled task (cron).\nCurrent time: %s\nPlanned fire time: %s\n\n",
			now, exec.FireAt.Format(time.RFC3339)))
		if exec.TriggerInfo != "" {
			b.WriteString("Trigger context:\n\n```json\n")
			b.WriteString(exec.TriggerInfo)
			b.WriteString("\n```\n\n")
		}
		b.WriteString("Follow the system prompt to run the task and output a structured markdown report. Every figure must come from real tool-call results.")
	}
	return b.String()
}

func writeAuditWindows(b *strings.Builder, exec *po.Execution) {
	var trigger struct {
		Timezone string `json:"timezone"`
	}
	_ = json.Unmarshal([]byte(exec.TriggerInfo), &trigger)
	zone, err := time.LoadLocation(trigger.Timezone)
	if err != nil {
		zone = time.UTC
	}
	end := exec.FireAt.In(zone)
	start := end.AddDate(0, 0, -1)
	previous := start.AddDate(0, 0, -1)
	b.WriteString(fmt.Sprintf("Audit windows in UTC (half-open): current = [%s, %s), previous = [%s, %s). Use these literal timestamps even if execution was delayed.\n", start.UTC().Format("2006-01-02 15:04:05.000000"), end.UTC().Format("2006-01-02 15:04:05.000000"), previous.UTC().Format("2006-01-02 15:04:05.000000"), start.UTC().Format("2006-01-02 15:04:05.000000")))
}

// ScheduledTaskSeedPrompt is the default prompt for the demo scheduled task.
const ScheduledTaskSeedPrompt = `You own the daily MySQL data inspection. Do the following:
1. Use mysql_tool to count base tables in the current database: SELECT COUNT(*) AS table_count FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'.
2. Use mysql_tool to aggregate taskflow_changes by table_name and operation for the two UTC audit windows supplied in the trigger. Filter occurred_at >= window_start AND occurred_at < window_end. These transactional INSERT/DELETE audit events include rows changed by direct SQL.
3. Compare insert and delete counts, and net growth (inserts minus deletes), between the current window and the previous window. Never estimate deletions from MAX(id), ID gaps or current row counts.
4. List the audited tables and state that totals cover those tables since capture was installed. If no historical events exist, state that a complete baseline is not available; do not infer historical deletions.
5. Output table count, per-table inserted/deleted rows, the comparison, and recommendations. Use UTC event timestamps and the scheduled fire time, never NOW() as a substitute for the planned window.`

// ConditionTaskSeedPrompt is the default prompt for the demo condition task.
const ConditionTaskSeedPrompt = `You own the security audit of MySQL inserted records. Each time a business row is inserted you must:
1. Read the record content in the trigger event carefully (the title / content / source fields).
2. Decide whether the content contains a stored XSS payload (e.g. <script>, onerror=, the javascript: protocol, event-handler attribute injection), an SQL-injection payload, or any other persistence-level security risk.
3. When needed, use mysql_tool to re-check the record's real stored content, and redis_tool to see whether the cache already holds a related risk marker.
4. Output the report: risk level (high / medium / low / none), the exact payload fragments matched, an attack-scenario explanation, and remediation / filtering advice.
Your verdict must be explicit. Never fabricate content that is not in the record.`

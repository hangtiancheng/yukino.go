package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/llm"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	yukino_cache "github.com/hangtiancheng/yukino.go/libs/yukino_cache"
)

type ReportStore struct {
	dao    *dao.DAO
	group  *yukino_cache.Group
	dir    string
	nodeID string
}

func NewReportStore(d *dao.DAO, group *yukino_cache.Group, dir, nodeID string) *ReportStore {
	return &ReportStore{dao: d, group: group, dir: dir, nodeID: nodeID}
}

func ReportCacheKey(executionID uint) string {
	return fmt.Sprintf("exec:%d", executionID)
}

type ReportInput struct {
	Execution *po.Execution
	Model     string
	TraceID   string
	Result    *llm.RunResult
	RunErr    error
	StartedAt time.Time
}

func (s *ReportStore) Render(in ReportInput) (path, body string, err error) {
	markdown := s.buildMarkdown(in)

	dayDir := filepath.Join(s.dir, in.StartedAt.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir report dir: %w", err)
	}
	relPath := filepath.Join(in.StartedAt.Format("2006-01-02"), fmt.Sprintf("exec-%d.md", in.Execution.ID))
	absPath := filepath.Join(s.dir, relPath)
	if err := writeAtomic(absPath, []byte(markdown)); err != nil {
		return "", "", err
	}
	return relPath, markdown, nil
}

func (s *ReportStore) Warm(ctx context.Context, executionID uint, body string) {
	if s.group != nil {
		if err := s.group.Set(ctx, ReportCacheKey(executionID), []byte(body)); err != nil {
			slog.Warn("warm report cache", "execution_id", executionID, "err", err)
		}
	}
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".report-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s *ReportStore) Load(ctx context.Context, executionID uint) (string, error) {
	if s.group != nil {
		view, err := s.group.Get(ctx, ReportCacheKey(executionID))
		if err == nil {
			return view.String(), nil
		}
	}
	exec, err := s.dao.GetExecution(ctx, executionID)
	if err != nil {
		return "", err
	}
	return exec.ReportBody, nil
}

func (s *ReportStore) buildMarkdown(in ReportInput) string {
	exec := in.Execution
	finishedAt := time.Now()
	status := po.StatusSucceeded
	if in.RunErr != nil {
		status = po.StatusFailed
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("execution_id: %d\n", exec.ID))
	b.WriteString(fmt.Sprintf("task_type: %s\n", exec.TaskType))
	b.WriteString(fmt.Sprintf("task_id: %d\n", exec.TaskID))
	b.WriteString(fmt.Sprintf("task_name: %s\n", quoteYAML(exec.TaskName)))
	b.WriteString(fmt.Sprintf("fire_key: %s\n", quoteYAML(exec.FireKey)))
	b.WriteString(fmt.Sprintf("status: %s\n", status))
	b.WriteString(fmt.Sprintf("node: %s\n", quoteYAML(s.nodeID)))
	if in.TraceID != "" {
		b.WriteString(fmt.Sprintf("trace_id: %s\n", in.TraceID))
	}
	b.WriteString(fmt.Sprintf("fire_at: %s\n", exec.FireAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("started_at: %s\n", in.StartedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("finished_at: %s\n", finishedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("duration_ms: %d\n", finishedAt.Sub(in.StartedAt).Milliseconds()))
	if in.Result != nil {
		b.WriteString(fmt.Sprintf("model: %s\n", quoteYAML(in.Model)))
		b.WriteString(fmt.Sprintf("llm_rounds: %d\n", in.Result.Rounds))
		b.WriteString(fmt.Sprintf("tool_calls: %d\n", len(in.Result.ToolCalls)))
		b.WriteString(fmt.Sprintf("tokens_prompt: %d\n", in.Result.TokensPrompt))
		b.WriteString(fmt.Sprintf("tokens_output: %d\n", in.Result.TokensOutput))
	}
	b.WriteString("---\n\n")

	b.WriteString(fmt.Sprintf("# Execution Report #%d - %s\n\n", exec.ID, exec.TaskName))

	if in.RunErr != nil {
		b.WriteString("## Execution Failed\n\n")
		b.WriteString(fmt.Sprintf("Error: `%s`\n\n", in.RunErr.Error()))
	}

	if in.Result != nil && in.Result.Markdown != "" {
		b.WriteString(in.Result.Markdown)
		b.WriteString("\n\n")
	}

	if in.Execution.TriggerInfo != "" {
		b.WriteString("## Trigger Context\n\n")
		b.WriteString("```json\n")
		b.WriteString(in.Execution.TriggerInfo)
		b.WriteString("\n```\n\n")
	}

	if in.Result != nil && len(in.Result.ToolCalls) > 0 {
		b.WriteString("## Appendix: Tool Call Log\n\n")
		b.WriteString("| Round | Tool | Args | Duration(ms) | Result Summary |\n")
		b.WriteString("| --- | --- | --- | --- | --- |\n")
		for _, call := range in.Result.ToolCalls {
			result := strings.ReplaceAll(call.Result, "\n", " ")
			result = strings.ReplaceAll(result, "|", "\\|")
			result = truncateRunes(result, 160)
			args := strings.ReplaceAll(string(call.Args), "|", "\\|")
			args = truncateRunes(args, 120)
			mark := "ok"
			if call.Error {
				mark = "error"
			}
			b.WriteString(fmt.Sprintf("| %d | %s | `%s` | %d | %s: %s |\n",
				call.Round, call.Name, args, call.Duration.Milliseconds(), mark, result))
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("*Generated by taskflow node `%s` at %s*\n", s.nodeID, finishedAt.Format(time.RFC3339)))
	return b.String()
}

func quoteYAML(s string) string { return strconv.Quote(s) }

func excerpt(s string, max int) string {
	return truncateRunes(s, max)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

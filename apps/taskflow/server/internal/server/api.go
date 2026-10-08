package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/cdc"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/cronx"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/engine"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/telemetry"
	yukino_cache "github.com/hangtiancheng/yukino.go/libs/yukino_cache"
	yukino "github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"gorm.io/gorm"
)

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ok(ctx *yukino.Context, data any) {
	ctx.JSON(yukino.H{"message": "ok", "data": data})
}

func fail(ctx *yukino.Context, status int, msg string) {
	ctx.Throw(status, msg)
}

func parseID(ctx *yukino.Context, key string) (uint, bool) {
	id, err := strconv.ParseUint(ctx.Param(key), 10, 64)
	if err != nil || id == 0 {
		fail(ctx, http.StatusBadRequest, "invalid "+key)
		return 0, false
	}
	return uint(id), true
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

// ------------------------------------------------------------------ overview

func (s *Service) handleHealth(ctx *yukino.Context, _ func()) {
	ok(ctx, yukino.H{
		"status": "up",
		"node":   s.cfg.Node.ID,
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Service) handleReady(ctx *yukino.Context, _ func()) {
	checkCtx, cancel := context.WithTimeout(ctx.Request.Context(), 3*time.Second)
	defer cancel()
	if s.ready != nil {
		if err := s.ready(checkCtx); err != nil {
			fail(ctx, http.StatusServiceUnavailable, "dependencies unavailable")
			return
		}
	}
	ok(ctx, yukino.H{"status": "ready", "node": s.cfg.Node.ID})
}

func (s *Service) handleOverview(ctx *yukino.Context, _ func()) {
	reqCtx := ctx.Request.Context()
	dayStart := time.Now().UTC().Truncate(24 * time.Hour)

	stats, err := s.dao.ExecutionStats(reqCtx, dayStart)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, "load stats: "+err.Error())
		return
	}

	scheduledTasks, err := s.dao.ListScheduledTasks(reqCtx)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, "load scheduled tasks: "+err.Error())
		return
	}
	conditionTasks, err := s.dao.ListConditionTasks(reqCtx)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, "load condition tasks: "+err.Error())
		return
	}

	recent, _, err := s.dao.ListExecutions(reqCtx, dao.ExecutionFilter{Page: 1, PageSize: 8})
	if err != nil {
		fail(ctx, http.StatusInternalServerError, "load executions: "+err.Error())
		return
	}

	countEnabled := func(sched []*po.ScheduledTask, cond []*po.ConditionTask) (int, int) {
		se, ce := 0, 0
		for _, t := range sched {
			if t.Enabled {
				se++
			}
		}
		for _, t := range cond {
			if t.Enabled {
				ce++
			}
		}
		return se, ce
	}
	se, ce := countEnabled(scheduledTasks, conditionTasks)

	ok(ctx, yukino.H{
		"today_status_counts": stats,
		"scheduled_total":     len(scheduledTasks),
		"scheduled_enabled":   se,
		"condition_total":     len(conditionTasks),
		"condition_enabled":   ce,
		"recent_executions":   recent,
		"nodes":               s.registry.Members(reqCtx),
		"node":                s.cfg.Node.ID,
	})
}

// ---------------------------------------------------------- scheduled tasks

type scheduledTaskPayload struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CronExpr    string  `json:"cron_expr"`
	Timezone    string  `json:"timezone"`
	Prompt      string  `json:"prompt"`
	Model       *string `json:"model"`
	Enabled     *bool   `json:"enabled"`
}

func (s *Service) handleListScheduled(ctx *yukino.Context, _ func()) {
	rows, err := s.dao.ListScheduledTasks(ctx.Request.Context())
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	type item struct {
		*po.ScheduledTask
		NextFireHint string `json:"next_fire_hint"`
	}
	items := make([]item, 0, len(rows))
	for _, row := range rows {
		hint := ""
		if sched, err := cronx.Parse(row.CronExpr); err == nil {
			loc, err := time.LoadLocation(row.Timezone)
			if err != nil {
				loc = time.UTC
			}
			if next := sched.Next(time.Now().In(loc)); !next.IsZero() {
				hint = next.Format(time.RFC3339)
			}
		}
		items = append(items, item{ScheduledTask: row, NextFireHint: hint})
	}
	ok(ctx, items)
}

func (s *Service) handleCreateScheduled(ctx *yukino.Context, _ func()) {
	var payload scheduledTaskPayload
	if err := ctx.BindJSON(&payload); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(payload.Name) == "" || strings.TrimSpace(payload.CronExpr) == "" || strings.TrimSpace(payload.Prompt) == "" {
		fail(ctx, http.StatusBadRequest, "name, cron_expr and prompt are required")
		return
	}
	if _, err := cronx.Parse(payload.CronExpr); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid cron expression: "+err.Error())
		return
	}

	tz := payload.Timezone
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		fail(ctx, http.StatusBadRequest, "invalid timezone: "+err.Error())
		return
	}

	row := &po.ScheduledTask{
		Name:        strings.TrimSpace(payload.Name),
		Description: stringValue(payload.Description),
		CronExpr:    strings.TrimSpace(payload.CronExpr),
		Timezone:    tz,
		Prompt:      payload.Prompt,
		Model:       stringValue(payload.Model),
		Enabled:     payload.Enabled != nil && *payload.Enabled,
	}
	if next := nextFireOrNil(row.CronExpr, time.Now().In(loc)); !next.IsZero() {
		nextUTC := next.UTC()
		row.NextFireAt = &nextUTC
	}

	if err := s.dao.CreateScheduledTask(ctx.Request.Context(), row); err != nil {
		if dao.IsDuplicateEntry(err) {
			fail(ctx, http.StatusConflict, "task name already exists")
			return
		}
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ctx.SetStatus(http.StatusCreated)
	ok(ctx, row)
}

// nextFireOrNil computes the next fire time for expr, returning the zero time
// when the expression is unparsable. Definitions are validated on write, but
// a hand-edited DB row must never panic the API.
func nextFireOrNil(expr string, after time.Time) time.Time {
	sched, err := cronx.Parse(expr)
	if err != nil {
		return time.Time{}
	}
	return sched.Next(after)
}

func (s *Service) handleGetScheduled(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	task, useCache, err := s.defs.GetScheduledTaskCached(ctx.Request.Context(), id)
	if err != nil {
		if dao.IsNotFound(err) {
			fail(ctx, http.StatusNotFound, "task not found")
			return
		}
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, yukino.H{"task": task, "from_cache": useCache})
}

func (s *Service) handleUpdateScheduled(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	var payload scheduledTaskPayload
	if err := ctx.BindJSON(&payload); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := s.dao.GetScheduledTask(ctx.Request.Context(), id)
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}

	if payload.Name != "" {
		existing.Name = strings.TrimSpace(payload.Name)
	}
	if payload.CronExpr != "" {
		if _, err := cronx.Parse(payload.CronExpr); err != nil {
			fail(ctx, http.StatusBadRequest, "invalid cron expression: "+err.Error())
			return
		}
		existing.CronExpr = strings.TrimSpace(payload.CronExpr)
	}
	if payload.Timezone != "" {
		if _, err := time.LoadLocation(payload.Timezone); err != nil {
			fail(ctx, http.StatusBadRequest, "invalid timezone: "+err.Error())
			return
		}
		existing.Timezone = payload.Timezone
	}
	if payload.Prompt != "" {
		existing.Prompt = payload.Prompt
	}
	if payload.Description != nil {
		existing.Description = *payload.Description
	}
	if payload.Model != nil {
		existing.Model = *payload.Model
	}
	if payload.Enabled != nil {
		existing.Enabled = *payload.Enabled
	}

	loc, err := time.LoadLocation(existing.Timezone)
	if err != nil {
		loc = time.Local
	}
	if next := nextFireOrNil(existing.CronExpr, time.Now().In(loc)); !next.IsZero() {
		nextUTC := next.UTC()
		existing.NextFireAt = &nextUTC
	}

	if err := s.defs.UpdateScheduledTask(ctx.Request.Context(), existing); err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, existing)
}

func (s *Service) handleDeleteScheduled(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	if err := s.dao.DeleteScheduledTask(ctx.Request.Context(), id); err != nil {
		s.notFoundOr500(ctx, err)
		return
	}
	s.defs.InvalidateScheduledTask(ctx.Request.Context(), id)
	ok(ctx, yukino.H{"deleted": id})
}

func (s *Service) handleTriggerScheduled(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	reqCtx := ctx.Request.Context()

	task, err := s.dao.GetScheduledTask(reqCtx, id)
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}

	now := time.Now()
	fireKey := fmt.Sprintf("manual:%d:%s:%s", task.ID, now.Format("20060102150405"), randomHex(4))
	if key := ctx.Get("Idempotency-Key"); key != "" {
		if len(key) > 200 {
			fail(ctx, http.StatusBadRequest, "idempotency key is too long")
			return
		}
		sum := sha256.Sum256([]byte(key))
		fireKey = fmt.Sprintf("manual:%d:%x", task.ID, sum)
	}
	triggerInfo, _ := json.Marshal(map[string]any{
		"trigger":  "manual",
		"node":     s.cfg.Node.ID,
		"timezone": task.Timezone,
		"trace_id": telemetry.TraceIDFrom(reqCtx),
	})

	exec := &po.Execution{
		TaskType:       po.TaskTypeManual,
		TaskID:         task.ID,
		TaskName:       task.Name,
		FireKey:        fireKey,
		Status:         po.StatusPending,
		FireAt:         now,
		TriggerInfo:    string(triggerInfo),
		PromptSnapshot: task.Prompt,
		ModelSnapshot:  task.Model,
	}
	if _, execID, err := s.dao.InsertPending(reqCtx, exec); err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	} else {
		exec.ID = execID
	}

	dispatched, err := s.dispatcher.Dispatch(reqCtx, exec)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, "dispatch failed: "+err.Error())
		return
	}
	ok(ctx, yukino.H{
		"execution_id": exec.ID,
		"fire_key":     fireKey,
		"dispatched":   dispatched,
		"trace_id":     telemetry.TraceIDFrom(reqCtx),
	})
}

// ---------------------------------------------------------- condition tasks

type conditionTaskPayload struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	TableName   string  `json:"table_name"`
	Prompt      string  `json:"prompt"`
	Model       *string `json:"model"`
	Enabled     *bool   `json:"enabled"`
}

func (s *Service) handleListCondition(ctx *yukino.Context, _ func()) {
	rows, err := s.dao.ListConditionTasks(ctx.Request.Context())
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, rows)
}

func (s *Service) handleCreateCondition(ctx *yukino.Context, _ func()) {
	var payload conditionTaskPayload
	if err := ctx.BindJSON(&payload); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(payload.Name) == "" || strings.TrimSpace(payload.TableName) == "" || strings.TrimSpace(payload.Prompt) == "" {
		fail(ctx, http.StatusBadRequest, "name, table_name and prompt are required")
		return
	}

	row := &po.ConditionTask{
		Name:        strings.TrimSpace(payload.Name),
		Description: stringValue(payload.Description),
		EventType:   po.EventTypeMySQLInsert,
		WatchTable:  strings.TrimSpace(payload.TableName),
		Prompt:      payload.Prompt,
		Model:       stringValue(payload.Model),
		Enabled:     payload.Enabled != nil && *payload.Enabled,
	}
	if !cdc.ValidWatchTable(row.WatchTable) {
		fail(ctx, http.StatusBadRequest, "control tables cannot be watched")
		return
	}
	if err := cdc.EnsureCapture(ctx.Request.Context(), s.dao.DB(), row.WatchTable); err != nil {
		fail(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.dao.CreateConditionTask(ctx.Request.Context(), row); err != nil {
		if dao.IsDuplicateEntry(err) {
			fail(ctx, http.StatusConflict, "task name already exists")
			return
		}
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ctx.SetStatus(http.StatusCreated)
	ok(ctx, row)
}

func (s *Service) handleGetCondition(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	task, useCache, err := s.defs.GetConditionTaskCached(ctx.Request.Context(), id)
	if err != nil {
		if dao.IsNotFound(err) {
			fail(ctx, http.StatusNotFound, "task not found")
			return
		}
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, yukino.H{"task": task, "from_cache": useCache})
}

func (s *Service) handleUpdateCondition(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	var payload conditionTaskPayload
	if err := ctx.BindJSON(&payload); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	existing, err := s.dao.GetConditionTask(ctx.Request.Context(), id)
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}
	if payload.Name != "" {
		existing.Name = strings.TrimSpace(payload.Name)
	}
	if payload.TableName != "" {
		existing.WatchTable = strings.TrimSpace(payload.TableName)
	}
	if payload.Prompt != "" {
		existing.Prompt = payload.Prompt
	}
	if payload.Description != nil {
		existing.Description = *payload.Description
	}
	if payload.Model != nil {
		existing.Model = *payload.Model
	}
	if payload.Enabled != nil {
		existing.Enabled = *payload.Enabled
	}

	if !cdc.ValidWatchTable(existing.WatchTable) {
		fail(ctx, http.StatusBadRequest, "control tables cannot be watched")
		return
	}
	if err := cdc.EnsureCapture(ctx.Request.Context(), s.dao.DB(), existing.WatchTable); err != nil {
		fail(ctx, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.defs.UpdateConditionTask(ctx.Request.Context(), existing); err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, existing)
}

func (s *Service) handleDeleteCondition(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	if err := s.dao.DeleteConditionTask(ctx.Request.Context(), id); err != nil {
		s.notFoundOr500(ctx, err)
		return
	}
	s.defs.InvalidateConditionTask(ctx.Request.Context(), id)
	ok(ctx, yukino.H{"deleted": id})
}

// handleTestCondition runs a condition task against an existing (or the
// latest) risk record with a fresh test fire key, bypassing the insert event.
func (s *Service) handleTestCondition(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	reqCtx := ctx.Request.Context()

	task, err := s.dao.GetConditionTask(reqCtx, id)
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}

	if task.WatchTable != "risk_records" {
		fail(ctx, http.StatusBadRequest, "sample testing is available for risk_records; insert into the watched table to trigger other tasks")
		return
	}
	var payload struct {
		RecordID uint `json:"record_id"`
	}
	if err := ctx.BindJSON(&payload); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid JSON")
		return
	}

	var record *po.RiskRecord
	if payload.RecordID != 0 {
		record, err = s.dao.GetRiskRecord(reqCtx, payload.RecordID)
	} else {
		records, _, err2 := s.dao.ListRiskRecords(reqCtx, 1, 1)
		err = err2
		if err == nil && len(records) > 0 {
			record = records[0]
		} else if err == nil {
			err = gorm.ErrRecordNotFound
		}
	}
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}

	now := time.Now()
	fireKey := fmt.Sprintf("condtest:%d:%d:%s", task.ID, record.ID, randomHex(6))
	if key := ctx.Get("Idempotency-Key"); key != "" {
		if len(key) > 200 {
			fail(ctx, http.StatusBadRequest, "idempotency key is too long")
			return
		}
		sum := sha256.Sum256([]byte(key))
		fireKey = fmt.Sprintf("condtest:%d:%d:%x", task.ID, record.ID, sum)
	}
	recordJSON, _ := json.Marshal(record)
	triggerInfo, _ := json.Marshal(map[string]any{
		"event_type":  po.EventTypeMySQLInsert,
		"table":       task.WatchTable,
		"record_id":   record.ID,
		"occurred_at": now.Format(time.RFC3339),
		"record":      json.RawMessage(recordJSON),
		"test":        true,
	})

	exec := &po.Execution{
		TaskType:       po.TaskTypeCondition,
		TaskID:         task.ID,
		TaskName:       task.Name,
		FireKey:        fireKey,
		Status:         po.StatusPending,
		FireAt:         record.CreatedAt,
		TriggerInfo:    string(triggerInfo),
		PromptSnapshot: task.Prompt,
		ModelSnapshot:  task.Model,
	}
	_, execID, err := s.dao.InsertPending(reqCtx, exec)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	exec.ID = execID

	dispatched, err := s.dispatcher.Dispatch(reqCtx, exec)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, "dispatch failed: "+err.Error())
		return
	}
	ok(ctx, yukino.H{
		"execution_id": exec.ID,
		"record_id":    record.ID,
		"fire_key":     fireKey,
		"dispatched":   dispatched,
	})
}

// ---------------------------------------------------------------- executions

func (s *Service) handleListExecutions(ctx *yukino.Context, _ func()) {
	taskID, _ := strconv.ParseUint(ctx.Query("task_id"), 10, 64)
	page, _ := strconv.Atoi(ctx.Query("page"))
	pageSize, _ := strconv.Atoi(ctx.Query("page_size"))

	rows, total, err := s.dao.ListExecutions(ctx.Request.Context(), dao.ExecutionFilter{
		TaskType: ctx.Query("task_type"),
		TaskID:   uint(taskID),
		Status:   ctx.Query("status"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, yukino.H{"items": rows, "total": total, "page": page, "page_size": pageSize})
}

func (s *Service) handleGetExecution(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	exec, err := s.dao.GetExecution(ctx.Request.Context(), id)
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}
	ok(ctx, exec)
}

func (s *Service) handleGetReport(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	body, err := s.reports.Load(ctx.Request.Context(), id)
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}
	if body == "" {
		fail(ctx, http.StatusNotFound, "report not generated yet")
		return
	}
	ctx.Type = "text/markdown; charset=utf-8"
	ctx.Body = []byte(body)
}

func (s *Service) handleCancelExecution(ctx *yukino.Context, _ func()) {
	id, valid := parseID(ctx, "id")
	if !valid {
		return
	}
	from := []string{po.StatusPending, po.StatusReserved, po.StatusQueued}
	affected, err := s.dao.Transition(ctx.Request.Context(), id, from, po.StatusCancelled, map[string]any{
		"finished_at": time.Now(),
		"error":       "cancelled by user",
	})
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if affected == 0 {
		fail(ctx, http.StatusConflict, "execution cannot be cancelled in its current status")
		return
	}
	ok(ctx, yukino.H{"cancelled": id})
}

// ------------------------------------------------------------- risk records

func (s *Service) handleListRiskRecords(ctx *yukino.Context, _ func()) {
	page, _ := strconv.Atoi(ctx.Query("page"))
	pageSize, _ := strconv.Atoi(ctx.Query("page_size"))
	rows, total, err := s.dao.ListRiskRecords(ctx.Request.Context(), page, pageSize)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, yukino.H{"items": rows, "total": total})
}

func (s *Service) handleCreateRiskRecord(ctx *yukino.Context, _ func()) {
	var payload struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Source  string `json:"source"`
	}
	if err := ctx.BindJSON(&payload); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		fail(ctx, http.StatusBadRequest, "content is required")
		return
	}

	row := &po.RiskRecord{
		Title:   payload.Title,
		Content: payload.Content,
		Source:  payload.Source,
	}
	reqCtx := ctx.Request.Context()
	if err := s.dao.CreateRiskRecord(reqCtx, row); err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}

	ctx.SetStatus(http.StatusCreated)
	ok(ctx, yukino.H{
		"record":         row,
		"event_delivery": "durable",
		"trace_id":       telemetry.TraceIDFrom(reqCtx),
	})
}

// ------------------------------------------------------------------ monitor

func (s *Service) handleMonitorNodes(ctx *yukino.Context, _ func()) {
	ok(ctx, s.registry.Members(ctx.Request.Context()))
}

func (s *Service) handleMonitorStats(ctx *yukino.Context, _ func()) {
	stats, err := s.monitor.Stats(ctx.Request.Context())
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, stats)
}

func (s *Service) handleMonitorCache(ctx *yukino.Context, _ func()) {
	group := yukino_cache.GetGroup(s.cfg.Cache.ReportGroup)
	if group == nil {
		fail(ctx, http.StatusNotFound, "cache group not registered")
		return
	}
	ok(ctx, group.Stats())
}

func (s *Service) handleListDeadLetters(ctx *yukino.Context, _ func()) {
	page, _ := strconv.Atoi(ctx.Query("page"))
	pageSize, _ := strconv.Atoi(ctx.Query("page_size"))
	rows, total, err := s.dao.ListDeadLetters(ctx.Request.Context(), page, pageSize)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, yukino.H{"items": rows, "total": total})
}

// handleMonitorConsensus exposes the embedded raft ledger: role, term,
// commit index and the most recent applied entries.
func (s *Service) handleMonitorConsensus(ctx *yukino.Context, _ func()) {
	ok(ctx, s.ledger.Status())
}

// handleMonitorJournal looks one fire key up in the lsm_tree audit journal.
func (s *Service) handleMonitorJournal(ctx *yukino.Context, _ func()) {
	fireKey := strings.TrimSpace(ctx.Query("fire_key"))
	if fireKey == "" {
		fail(ctx, http.StatusBadRequest, "fire_key query parameter is required")
		return
	}
	entry, found, err := s.journal.Lookup(fireKey)
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		fail(ctx, http.StatusNotFound, "no journal entry for this fire key on this node")
		return
	}
	ok(ctx, entry)
}

// handleMonitorMirror exposes the redis execution-status mirror maintained by
// the monitor's MySQL -> redis sync pass.
func (s *Service) handleMonitorMirror(ctx *yukino.Context, _ func()) {
	mirror, err := s.monitor.StatusMirror(ctx.Request.Context())
	if err != nil {
		fail(ctx, http.StatusInternalServerError, err.Error())
		return
	}
	ok(ctx, yukino.H{"key": engine.ExecMirrorKey, "entries": len(mirror), "items": mirror})
}

// ------------------------------------------------------- internal endpoints

// handleFire is the distributed time-wheel callback. The wheel pops each due
// task atomically across the cluster, and this handler adds the idempotent
// dispatch funnel on top.
func (s *Service) handleFire(ctx *yukino.Context, _ func()) {
	var callback engine.FireCallback
	if err := ctx.BindJSON(&callback); err != nil {
		fail(ctx, http.StatusBadRequest, "invalid fire callback: "+err.Error())
		return
	}
	reqCtx := ctx.Request.Context()

	exec, err := s.dao.GetExecution(reqCtx, callback.ExecutionID)
	if err != nil {
		s.notFoundOr500(ctx, err)
		return
	}
	if exec.FireKey != callback.FireKey {
		fail(ctx, http.StatusBadRequest, "fire key mismatch")
		return
	}
	if exec.FireAt.After(time.Now()) {
		fail(ctx, http.StatusConflict, "execution is not due")
		return
	}

	dispatched, err := s.dispatcher.Dispatch(reqCtx, exec)
	if err != nil {
		ctx.SetStatus(http.StatusInternalServerError)
		ctx.JSON(yukino.H{"message": err.Error(), "data": nil})
		return
	}
	// Re-read so the response carries the post-dispatch status, not the
	// pre-dispatch snapshot.
	status := exec.Status
	if fresh, ferr := s.dao.GetExecution(reqCtx, exec.ID); ferr == nil {
		status = fresh.Status
	}
	ok(ctx, yukino.H{
		"execution_id": exec.ID,
		"dispatched":   dispatched,
		"status":       status,
	})
}

// handleTelemetryLog is the ingestion endpoint used as the dsn of the client
// @yukino.js/sentry SDK. Events are appended to a jsonl file for inspection.
func (s *Service) handleTelemetryLog(ctx *yukino.Context, _ func()) {
	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, 1<<20))
	if err != nil {
		fail(ctx, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	defer ctx.Request.Body.Close()
	if !json.Valid(body) {
		fail(ctx, http.StatusBadRequest, "invalid telemetry JSON")
		return
	}

	entry, _ := json.Marshal(map[string]any{
		"received_at": time.Now().Format(time.RFC3339Nano),
		"payload":     json.RawMessage(body),
	})
	s.telemetryLogMu.Lock()
	defer s.telemetryLogMu.Unlock()
	if s.telemetryLog != nil {
		if _, err := s.telemetryLog.Write(append(entry, '\n')); err != nil {
			slog.Warn("write telemetry log", "err", err)
		}
	}
	ok(ctx, yukino.H{"received": len(body)})
}

func (s *Service) notFoundOr500(ctx *yukino.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fail(ctx, http.StatusNotFound, "not found")
		return
	}
	fail(ctx, http.StatusInternalServerError, err.Error())
}

func (s *Service) Close() {
	if s.telemetryLog != nil {
		_ = s.telemetryLog.Close()
	}
}

func openTelemetryLog(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

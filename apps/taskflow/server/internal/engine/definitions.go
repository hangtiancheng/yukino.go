package engine

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/components/consistent_cache"
)

// DefinitionCache serves task definitions through consistent_cache services.
// The library keys cache entries by Object.Key(), which is the numeric row id
// for both tables, so each service is wrapped in a prefixedCache to keep the
// two key spaces apart.
type DefinitionCache struct {
	dao       *dao.DAO
	schedSvc  *consistent_cache.Service
	condSvc   *consistent_cache.Service
	schedKeys prefixedCache
	condKeys  prefixedCache
}

type prefixedCache struct {
	inner  consistent_cache.Cache
	prefix string
}

func (p prefixedCache) Enable(ctx context.Context, key string, delayMillis int64) error {
	return p.inner.Enable(ctx, p.prefix+key, delayMillis)
}

func (p prefixedCache) Disable(ctx context.Context, key string, expireSeconds int64) error {
	return p.inner.Disable(ctx, p.prefix+key, expireSeconds)
}

func (p prefixedCache) Get(ctx context.Context, key string) (string, error) {
	return p.inner.Get(ctx, p.prefix+key)
}

func (p prefixedCache) Del(ctx context.Context, key string) error {
	return p.inner.Del(ctx, p.prefix+key)
}

func (p prefixedCache) PutWhenEnable(ctx context.Context, key, value string, expireSeconds int64) (bool, error) {
	return p.inner.PutWhenEnable(ctx, p.prefix+key, value, expireSeconds)
}

func NewDefinitionCache(d *dao.DAO, schedCache, condCache consistent_cache.Cache, db consistent_cache.DB, opts ...consistent_cache.Option) *DefinitionCache {
	schedKeys := prefixedCache{inner: schedCache, prefix: "taskflow:def:sched:"}
	condKeys := prefixedCache{inner: condCache, prefix: "taskflow:def:cond:"}
	return &DefinitionCache{
		dao:       d,
		schedSvc:  consistent_cache.NewService(schedKeys, db, opts...),
		condSvc:   consistent_cache.NewService(condKeys, db, opts...),
		schedKeys: schedKeys,
		condKeys:  condKeys,
	}
}

// Definition is the subset of a task definition the executor needs.
type Definition struct {
	Name   string
	Prompt string
	Model  string
}

func (c *DefinitionCache) LoadDefinition(ctx context.Context, exec *po.Execution) (*Definition, error) {
	if exec.PromptSnapshot != "" {
		return &Definition{Name: exec.TaskName, Prompt: exec.PromptSnapshot, Model: exec.ModelSnapshot}, nil
	}
	switch exec.TaskType {
	case po.TaskTypeCondition:
		obj := dao.NewConditionTaskObject(exec.TaskID)
		if _, err := c.condSvc.Get(ctx, obj); err != nil {
			return nil, fmt.Errorf("condition task %d: %w", exec.TaskID, err)
		}
		return &Definition{Name: obj.Name, Prompt: obj.Prompt, Model: obj.Model}, nil
	case po.TaskTypeScheduled, po.TaskTypeManual:
		obj := dao.NewScheduledTaskObject(exec.TaskID)
		if _, err := c.schedSvc.Get(ctx, obj); err != nil {
			return nil, fmt.Errorf("scheduled task %d: %w", exec.TaskID, err)
		}
		return &Definition{Name: obj.Name, Prompt: obj.Prompt, Model: obj.Model}, nil
	default:
		return nil, fmt.Errorf("unknown task type %q", exec.TaskType)
	}
}

// PutScheduledTask persists a definition through the cache-consistency flow
// (disable marker -> cache delete -> DB write -> delayed re-enable).
func (c *DefinitionCache) PutScheduledTask(ctx context.Context, task *po.ScheduledTask) error {
	return c.schedSvc.Put(ctx, &dao.ScheduledTaskObject{ScheduledTask: *task})
}

// GetScheduledTaskCached reads one definition through the cache-aside flow.
func (c *DefinitionCache) GetScheduledTaskCached(ctx context.Context, id uint) (*po.ScheduledTask, bool, error) {
	obj := dao.NewScheduledTaskObject(id)
	useCache, err := c.schedSvc.Get(ctx, obj)
	if err != nil {
		return nil, useCache, err
	}
	task := obj.ScheduledTask
	return &task, useCache, nil
}

// UpdateScheduledTask mirrors the library write path for updates that must
// persist zero values (enabled=false): it drops the cache under a short
// disable marker, then writes through an explicit column map.
func (c *DefinitionCache) UpdateScheduledTask(ctx context.Context, task *po.ScheduledTask) error {
	key := fmt.Sprintf("%d", task.ID)
	_ = c.schedKeys.Disable(ctx, key, 1)
	_ = c.schedKeys.Del(ctx, key)
	return c.dao.UpdateScheduledTask(ctx, task)
}

// GetConditionTaskCached reads one condition definition through the cache.
func (c *DefinitionCache) GetConditionTaskCached(ctx context.Context, id uint) (*po.ConditionTask, bool, error) {
	obj := dao.NewConditionTaskObject(id)
	useCache, err := c.condSvc.Get(ctx, obj)
	if err != nil {
		return nil, useCache, err
	}
	task := obj.ConditionTask
	return &task, useCache, nil
}

// UpdateConditionTask is the zero-value-safe update path for conditions.
func (c *DefinitionCache) UpdateConditionTask(ctx context.Context, task *po.ConditionTask) error {
	key := fmt.Sprintf("%d", task.ID)
	_ = c.condKeys.Disable(ctx, key, 1)
	_ = c.condKeys.Del(ctx, key)
	return c.dao.UpdateConditionTask(ctx, task)
}

// PutConditionTask does the same for condition definitions.
func (c *DefinitionCache) PutConditionTask(ctx context.Context, task *po.ConditionTask) error {
	return c.condSvc.Put(ctx, &dao.ConditionTaskObject{ConditionTask: *task})
}

// InvalidateScheduledTask drops the cached entry after a definition delete.
func (c *DefinitionCache) InvalidateScheduledTask(ctx context.Context, id uint) {
	_ = c.schedKeys.Del(ctx, fmt.Sprintf("%d", id))
}

// InvalidateConditionTask drops the cached entry after a definition delete.
func (c *DefinitionCache) InvalidateConditionTask(ctx context.Context, id uint) {
	_ = c.condKeys.Del(ctx, fmt.Sprintf("%d", id))
}

// SyncDrift reconciles the redis definition cache against MySQL: any cached
// entry whose payload diverges from the DB row is dropped under a short
// disable marker so the next read re-hydrates from the source of truth.
// Writes that bypass the cache-aware path (migrator bookkeeping, direct SQL)
// are the main drift source; the monitor sweep calls this periodically.
// Returns the number of repaired (dropped) entries.
func (c *DefinitionCache) SyncDrift(ctx context.Context) (int, error) {
	repaired := 0

	schedRows, err := c.dao.ListScheduledTasks(ctx)
	if err != nil {
		return 0, fmt.Errorf("list scheduled tasks: %w", err)
	}
	for _, row := range schedRows {
		obj := &dao.ScheduledTaskObject{ScheduledTask: *row}
		if c.repair(ctx, c.schedKeys, obj.Key(), obj) {
			repaired++
		}
	}

	condRows, err := c.dao.ListConditionTasks(ctx)
	if err != nil {
		return repaired, fmt.Errorf("list condition tasks: %w", err)
	}
	for _, row := range condRows {
		obj := &dao.ConditionTaskObject{ConditionTask: *row}
		if c.repair(ctx, c.condKeys, obj.Key(), obj) {
			repaired++
		}
	}
	return repaired, nil
}

// repair compares one cached definition with its DB shape and drops the entry
// on divergence. Returns true when an entry was repaired.
func (c *DefinitionCache) repair(ctx context.Context, keys prefixedCache, key string, obj interface {
	Write() (string, error)
}) bool {
	cached, err := keys.Get(ctx, key)
	if err != nil || cached == "" {
		return false // not cached (or cache error): nothing to repair
	}
	fresh, err := obj.Write()
	if err != nil {
		return false
	}
	if cached == fresh {
		return false
	}
	_ = keys.Disable(ctx, key, 1)
	_ = keys.Del(ctx, key)
	return true
}

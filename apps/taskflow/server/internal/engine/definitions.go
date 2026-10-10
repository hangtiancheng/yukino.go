package engine

import (
	"context"
	"fmt"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/components/consistent_cache"
)

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

func (c *DefinitionCache) PutScheduledTask(ctx context.Context, task *po.ScheduledTask) error {
	return c.schedSvc.Put(ctx, &dao.ScheduledTaskObject{ScheduledTask: *task})
}

func (c *DefinitionCache) GetScheduledTaskCached(ctx context.Context, id uint) (*po.ScheduledTask, bool, error) {
	obj := dao.NewScheduledTaskObject(id)
	useCache, err := c.schedSvc.Get(ctx, obj)
	if err != nil {
		return nil, useCache, err
	}
	task := obj.ScheduledTask
	return &task, useCache, nil
}

func (c *DefinitionCache) UpdateScheduledTask(ctx context.Context, task *po.ScheduledTask) error {
	key := fmt.Sprintf("%d", task.ID)
	_ = c.schedKeys.Disable(ctx, key, 1)
	_ = c.schedKeys.Del(ctx, key)
	return c.dao.UpdateScheduledTask(ctx, task)
}

func (c *DefinitionCache) GetConditionTaskCached(ctx context.Context, id uint) (*po.ConditionTask, bool, error) {
	obj := dao.NewConditionTaskObject(id)
	useCache, err := c.condSvc.Get(ctx, obj)
	if err != nil {
		return nil, useCache, err
	}
	task := obj.ConditionTask
	return &task, useCache, nil
}

func (c *DefinitionCache) UpdateConditionTask(ctx context.Context, task *po.ConditionTask) error {
	key := fmt.Sprintf("%d", task.ID)
	_ = c.condKeys.Disable(ctx, key, 1)
	_ = c.condKeys.Del(ctx, key)
	return c.dao.UpdateConditionTask(ctx, task)
}

func (c *DefinitionCache) PutConditionTask(ctx context.Context, task *po.ConditionTask) error {
	return c.condSvc.Put(ctx, &dao.ConditionTaskObject{ConditionTask: *task})
}

func (c *DefinitionCache) InvalidateScheduledTask(ctx context.Context, id uint) {
	_ = c.schedKeys.Del(ctx, fmt.Sprintf("%d", id))
}

func (c *DefinitionCache) InvalidateConditionTask(ctx context.Context, id uint) {
	_ = c.condKeys.Del(ctx, fmt.Sprintf("%d", id))
}

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

func (c *DefinitionCache) repair(ctx context.Context, keys prefixedCache, key string, obj interface {
	Write() (string, error)
}) bool {
	cached, err := keys.Get(ctx, key)
	if err != nil || cached == "" {
		return false
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

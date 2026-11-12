package time_wheel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	time_wheel_http "github.com/hangtiancheng/yukino.go/components/time_wheel/pkg/http"
	"github.com/hangtiancheng/yukino.go/components/time_wheel/pkg/redis"
)

type RTaskElement struct {
	Key         string            `json:"key"`
	CallbackURL string            `json:"callback_url"`
	Method      string            `json:"method"`
	Req         any               `json:"req"`
	Header      map[string]string `json:"header"`
}

type RTimeWheel struct {
	sync.Once
	redisClient *redis.Client
	httpClient  *time_wheel_http.Client
	stopChan    chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	ticker      *time.Ticker
}

func NewRTimeWheel(redisClient *redis.Client, httpClient *time_wheel_http.Client) *RTimeWheel {
	ctx, cancel := context.WithCancel(context.Background())
	r := RTimeWheel{
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
		ticker:      time.NewTicker(time.Second),
		redisClient: redisClient,
		httpClient:  httpClient,
		stopChan:    make(chan struct{}),
	}

	go r.run()
	return &r
}

func (r *RTimeWheel) Stop() {
	r.Do(func() {
		r.cancel()
		close(r.stopChan)
		r.ticker.Stop()
	})
	<-r.done
}

func (r *RTimeWheel) AddTask(ctx context.Context, key string, task *RTaskElement, executeAt time.Time) error {
	if ctx == nil || r.redisClient == nil {
		return fmt.Errorf("context and redis client are required")
	}
	if key == "" {
		return fmt.Errorf("empty task key")
	}
	if err := r.addTaskPreCheck(task); err != nil {
		return err
	}

	taskCopy := *task
	taskCopy.Key = key
	taskBody, err := json.Marshal(&taskCopy)
	if err != nil {
		return fmt.Errorf("marshal task %s: %w", key, err)
	}
	_, err = r.redisClient.Eval(ctx, LuaAddTasks, 2, []any{
		r.getTaskQueueKey(),
		r.getTaskIndexKey(),
		executeAt.Unix(),
		string(taskBody),
		key,
	})
	return err
}

func (r *RTimeWheel) RemoveTask(ctx context.Context, key string, executeAt time.Time) error {
	if ctx == nil || r.redisClient == nil {
		return fmt.Errorf("context and redis client are required")
	}
	if key == "" {
		return fmt.Errorf("empty task key")
	}
	_, err := r.redisClient.Eval(ctx, LuaDeleteTask, 2, []any{
		r.getTaskQueueKey(),
		r.getTaskIndexKey(),
		key,
	})
	return err
}

func (r *RTimeWheel) run() {
	defer close(r.done)
	for {
		select {
		case <-r.stopChan:
			return
		case <-r.ticker.C:
			r.executeTasks()
		}
	}
}

func (r *RTimeWheel) executeTasks() {
	defer func() {
		if err := recover(); err != nil {
			slog.Error("execute tasks panicked", "panic", err)
		}
	}()
	if r.redisClient == nil || r.httpClient == nil {
		slog.Error("time wheel requires redis and http clients")
		return
	}

	ctxWithTimeout, cancel := context.WithTimeout(r.ctx, time.Second*30)
	defer cancel()
	tasks, err := r.getExecutableTasks(ctxWithTimeout)
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	jobs := make(chan *RTaskElement)
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				if err := r.executeTask(ctxWithTimeout, task); err != nil {
					slog.Error("execute task failed", "error", err)
				}
			}
		}()
	}
	for _, task := range tasks {
		select {
		case jobs <- task:
		case <-ctxWithTimeout.Done():
		}
	}
	close(jobs)
	wg.Wait()
}

func (r *RTimeWheel) executeTask(ctx context.Context, task *RTaskElement) (err error) {
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("time wheel callback panicked: %v", fault)
		}
	}()
	if err := r.addTaskPreCheck(task); err != nil {
		return err
	}
	if r.httpClient == nil {
		return fmt.Errorf("nil http client")
	}
	return r.httpClient.JSONDo(ctx, task.Method, task.CallbackURL, task.Header, task.Req, nil)
}

func (r *RTimeWheel) addTaskPreCheck(task *RTaskElement) error {
	if task == nil {
		return fmt.Errorf("nil task")
	}
	if task.Method != http.MethodGet && task.Method != http.MethodPost {
		return fmt.Errorf("invalid method: %s", task.Method)
	}
	if !strings.HasPrefix(task.CallbackURL, "http://") && !strings.HasPrefix(task.CallbackURL, "https://") {
		return fmt.Errorf("invalid url: %s", task.CallbackURL)
	}
	return nil
}

func (r *RTimeWheel) getExecutableTasks(ctx context.Context) ([]*RTaskElement, error) {
	now := time.Now()
	rawReply, err := r.redisClient.Eval(ctx, LuaZrangeTasks, 2, []any{
		r.getTaskQueueKey(), r.getTaskIndexKey(), now.Unix(),
	})
	if err != nil {
		return nil, err
	}

	replies, ok := rawReply.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid replies: %T", rawReply)
	}

	tasks := make([]*RTaskElement, 0, len(replies))
	for i := range replies {
		var task RTaskElement
		if err := json.Unmarshal([]byte(toString(replies[i])), &task); err != nil {
			return nil, fmt.Errorf("unmarshal claimed time wheel task: %w", err)
		}
		if task.Key == "" {
			return nil, fmt.Errorf("claimed time wheel task has no key")
		}
		tasks = append(tasks, &task)
	}

	return tasks, nil
}

func (r *RTimeWheel) getTaskQueueKey() string {
	return "yukino_time_wheel_tasks:{wheel}"
}

func (r *RTimeWheel) getTaskIndexKey() string {
	return "yukino_time_wheel_task_index:{wheel}"
}

func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

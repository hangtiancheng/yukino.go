package executor

import (
	"context"
	"encoding/json"
	"fmt"
	net_http "net/http"
	"strings"
	"time"

	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/vo"
	"github.com/hangtiancheng/yukino.go/components/timer/common/utils"
	task_dao "github.com/hangtiancheng/yukino.go/components/timer/internal/dao/task"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/bloom"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/log"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/promethus"
	"github.com/hangtiancheng/yukino.go/components/timer/pkg/xhttp"
)

type Worker struct {
	timerService *TimerService
	taskDAO      *task_dao.TaskDAO
	httpClient   *xhttp.JSONClient
	bloomFilter  *bloom.Filter
	reporter     *promethus.Reporter
}

func NewWorker(timerService *TimerService, taskDAO *task_dao.TaskDAO, httpClient *xhttp.JSONClient, bloomFilter *bloom.Filter, reporter *promethus.Reporter) *Worker {
	return &Worker{
		timerService: timerService,
		taskDAO:      taskDAO,
		httpClient:   httpClient,
		bloomFilter:  bloomFilter,
		reporter:     reporter,
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.timerService.Start(ctx)
}

func (w *Worker) Work(ctx context.Context, timerIDUnixKey string) error {
	timerID, unix, err := utils.SplitTimerIDUnix(timerIDUnixKey)
	if err != nil {
		return err
	}

	if exist, err := w.bloomFilter.Exist(ctx, utils.GetTaskBloomFilterKey(utils.GetDayStr(time.UnixMilli(unix))), timerIDUnixKey); err != nil || exist {
		log.WarnContextf(ctx, "bloom filter check failed, start to check db, bloom key: %s, timerIDUnixKey: %s, err: %v, exist: %t", utils.GetTaskBloomFilterKey(utils.GetDayStr(time.UnixMilli(unix))), timerIDUnixKey, err, exist)
		task, err := w.taskDAO.GetTask(ctx, task_dao.WithTimerID(timerID), task_dao.WithRunTimer(time.UnixMilli(unix)))
		if err == nil && task.Status != consts.NotRun.ToInt() {
			log.WarnContextf(ctx, "task is already executed, timerID: %d, exec_time: %v", timerID, task.RunTimer)
			return nil
		}
	}

	return w.executeAndPostProcess(ctx, timerID, unix)
}

func (w *Worker) executeAndPostProcess(ctx context.Context, timerID uint, unix int64) error {
	timer, err := w.timerService.GetTimer(ctx, timerID)
	if err != nil {
		return fmt.Errorf("get timer failed, id: %d, err: %w", timerID, err)
	}

	if timer.Status != consts.Enable {
		log.WarnContextf(ctx, "timer has already been unable, timerID: %d", timerID)
		return nil
	}

	execTime := time.Now()
	resp, err := w.execute(ctx, timer)
	return w.postProcess(ctx, resp, err, timer.App, timerID, unix, execTime)
}

func (w *Worker) execute(ctx context.Context, timer *vo.Timer) (map[string]any, error) {
	var (
		resp map[string]any
		err  error
	)
	switch strings.ToUpper(timer.NotifyHTTPParam.Method) {
	case net_http.MethodGet:
		err = w.httpClient.Get(ctx, timer.NotifyHTTPParam.URL, timer.NotifyHTTPParam.Header, nil, &resp)
	case net_http.MethodPatch:
		err = w.httpClient.Patch(ctx, timer.NotifyHTTPParam.URL, timer.NotifyHTTPParam.Header, timer.NotifyHTTPParam.Body, &resp)
	case net_http.MethodDelete:
		err = w.httpClient.Delete(ctx, timer.NotifyHTTPParam.URL, timer.NotifyHTTPParam.Header, timer.NotifyHTTPParam.Body, &resp)
	case net_http.MethodPost:
		err = w.httpClient.Post(ctx, timer.NotifyHTTPParam.URL, timer.NotifyHTTPParam.Header, timer.NotifyHTTPParam.Body, &resp)
	default:
		err = fmt.Errorf("invalid http method: %s, timer: %s", timer.NotifyHTTPParam.Method, timer.Name)
	}

	return resp, err
}

func (w *Worker) postProcess(ctx context.Context, resp map[string]any, execErr error, app string, timerID uint, unix int64, execTime time.Time) error {
	go w.reportMonitorData(app, unix, execTime)
	if err := w.bloomFilter.Set(ctx, utils.GetTaskBloomFilterKey(utils.GetDayStr(time.UnixMilli(unix))), utils.UnionTimerIDUnix(timerID, unix), consts.BloomFilterKeyExpireSeconds); err != nil {
		log.ErrorContextf(ctx, "set bloom filter failed, key: %s, err: %v", utils.GetTaskBloomFilterKey(utils.GetDayStr(time.UnixMilli(unix))), err)
	}

	task, err := w.taskDAO.GetTask(ctx, task_dao.WithTimerID(timerID), task_dao.WithRunTimer(time.UnixMilli(unix)))
	if err != nil {
		return fmt.Errorf("get task failed, timerID: %d, runTimer: %v, err: %w", timerID, time.UnixMilli(unix), err)
	}

	respBody, _ := json.Marshal(resp)
	task.Output = string(respBody)

	if execErr != nil {
		task.Status = consts.Failed.ToInt()
	} else {
		task.Status = consts.Succeed.ToInt()
	}

	return w.taskDAO.UpdateTask(ctx, task)
}

func (w *Worker) reportMonitorData(app string, expectExecTimeUnix int64, actualExecTime time.Time) {
	w.reporter.ReportExecRecord(app)
	w.reporter.ReportTimerDelayRecord(app, float64(actualExecTime.UnixMilli()-expectExecTimeUnix))
}

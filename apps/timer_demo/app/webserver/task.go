package webserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/model/vo"
	service "github.com/hangtiancheng/yukino.go/apps/timer_demo/service/webserver"

	yukino "github.com/hangtiancheng/yukino.go/yukino_http"
)

type TaskApp struct {
	service taskService
}

func NewTaskApp(service *service.TaskService) *TaskApp {
	return &TaskApp{service: service}
}

func (t *TaskApp) GetTasks(ctx *yukino.Context, next func()) {
	req, err := parseGetTasksReq(ctx)
	if err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[get tasks] bind req failed, err: %v", err)))
		return
	}

	tasks, total, err := t.service.GetTasks(ctx.Request.Context(), req)
	ctx.JSON(vo.NewGetTasksResp(tasks, total, vo.NewCodeMsgWithErr(err)))
}

func parseGetTasksReq(ctx *yukino.Context) (*vo.GetTasksReq, error) {
	req := &vo.GetTasksReq{}

	timerIDStr := ctx.Query("timerID")
	if timerIDStr == "" {
		return nil, fmt.Errorf("timerID is required")
	}
	timerID, err := strconv.ParseUint(timerIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timerID: %v", err)
	}
	req.TimerID = uint(timerID)

	if idx := ctx.Query("pageIndex"); idx != "" {
		req.Index, _ = strconv.Atoi(idx)
	}
	if sz := ctx.Query("pageSize"); sz != "" {
		req.Size, _ = strconv.Atoi(sz)
	}
	return req, nil
}

type taskService interface {
	GetTask(ctx context.Context, id uint) (*vo.Task, error)
	GetTasks(ctx context.Context, req *vo.GetTasksReq) ([]*vo.Task, int64, error)
}

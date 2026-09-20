// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

type TimerApp struct {
	service timerService
}

func NewTimerApp(service *service.TimerService) *TimerApp {
	return &TimerApp{service: service}
}

// CreateTimer creates a timer definition.
func (t *TimerApp) CreateTimer(ctx *yukino.Context, next func()) {
	var req vo.Timer
	if err := ctx.BindJSON(&req); err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[create timer] bind req failed, err: %v", err)))
		return
	}

	id, err := t.service.CreateTimer(ctx.Request.Context(), &req)
	if err != nil {
		ctx.JSON(vo.NewCodeMsg(-1, err.Error()))
		return
	}
	ctx.JSON(vo.NewCreateTimerResp(id, vo.NewCodeMsgWithErr(nil)))
}

// GetAppTimers returns all timers under an app.
func (t *TimerApp) GetAppTimers(ctx *yukino.Context, next func()) {
	req, err := parseGetAppTimersReq(ctx)
	if err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[get app timers] bind req failed, err: %v", err)))
		return
	}

	timers, total, err := t.service.GetAppTimers(ctx.Request.Context(), req)
	if err != nil {
		ctx.JSON(vo.NewCodeMsg(-1, err.Error()))
		return
	}
	ctx.JSON(vo.NewGetTimersResp(timers, total, vo.NewCodeMsgWithErr(nil)))
}

func (t *TimerApp) GetTimersByName(ctx *yukino.Context, next func()) {
	req, err := parseGetTimersByNameReq(ctx)
	if err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[get timers by name] bind req failed, err: %v", err)))
		return
	}

	timers, total, err := t.service.GetTimersByName(ctx.Request.Context(), req)
	if err != nil {
		ctx.JSON(vo.NewCodeMsg(-1, err.Error()))
		return
	}
	ctx.JSON(vo.NewGetTimersResp(timers, total, vo.NewCodeMsgWithErr(nil)))
}

func (t *TimerApp) DeleteTimer(ctx *yukino.Context, next func()) {
	var req vo.TimerReq
	if err := ctx.BindJSON(&req); err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[delete timer] bind req failed, err: %v", err)))
		return
	}

	if err := t.service.DeleteTimer(ctx.Request.Context(), req.App, req.ID); err != nil {
		ctx.JSON(vo.NewCodeMsg(-1, err.Error()))
		return
	}
	ctx.JSON(vo.NewCodeMsgWithErr(nil))
}

func (t *TimerApp) UpdateTimer(ctx *yukino.Context, next func()) {
	ctx.JSON(nil)
}

func (t *TimerApp) GetTimer(ctx *yukino.Context, next func()) {
	req, err := parseTimerReqFromQuery(ctx)
	if err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[get timer] bind req failed, err: %v", err)))
		return
	}

	timer, err := t.service.GetTimer(ctx.Request.Context(), req.ID)
	if err != nil {
		ctx.JSON(vo.NewCodeMsg(-1, err.Error()))
		return
	}
	ctx.JSON(vo.NewGetTimerResp(timer, vo.NewCodeMsgWithErr(nil)))
}

// EnableTimer activates a timer.
func (t *TimerApp) EnableTimer(ctx *yukino.Context, next func()) {
	var req vo.TimerReq
	if err := ctx.BindJSON(&req); err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[enable timer] bind req failed, err: %v", err)))
		return
	}

	if err := t.service.EnableTimer(ctx.Request.Context(), req.App, req.ID); err != nil {
		ctx.JSON(vo.NewCodeMsg(-1, err.Error()))
		return
	}
	ctx.JSON(vo.NewCodeMsgWithErr(nil))
}

// UnableTimer deactivates a timer.
func (t *TimerApp) UnableTimer(ctx *yukino.Context, next func()) {
	var req vo.TimerReq
	if err := ctx.BindJSON(&req); err != nil {
		ctx.SetStatus(http.StatusBadRequest)
		ctx.JSON(vo.NewCodeMsg(-1, fmt.Sprintf("[enable timer] bind req failed, err:%v", err)))
		return
	}

	if err := t.service.UnableTimer(ctx.Request.Context(), req.App, req.ID); err != nil {
		ctx.JSON(vo.NewCodeMsg(-1, err.Error()))
		return
	}
	ctx.JSON(vo.NewCodeMsgWithErr(nil))
}

func parseTimerReqFromQuery(ctx *yukino.Context) (*vo.TimerReq, error) {
	req := &vo.TimerReq{}
	req.App = ctx.Query("app")
	if req.App == "" {
		return nil, fmt.Errorf("app is required")
	}
	idStr := ctx.Query("id")
	if idStr == "" {
		return nil, fmt.Errorf("id is required")
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid id: %v", err)
	}
	req.ID = uint(id)
	return req, nil
}

func parseGetAppTimersReq(ctx *yukino.Context) (*vo.GetAppTimersReq, error) {
	req := &vo.GetAppTimersReq{}
	req.App = ctx.Query("app")
	if req.App == "" {
		return nil, fmt.Errorf("app is required")
	}
	if idx := ctx.Query("pageIndex"); idx != "" {
		req.Index, _ = strconv.Atoi(idx)
	}
	if sz := ctx.Query("pageSize"); sz != "" {
		req.Size, _ = strconv.Atoi(sz)
	}
	return req, nil
}

func parseGetTimersByNameReq(ctx *yukino.Context) (*vo.GetTimersByNameReq, error) {
	req := &vo.GetTimersByNameReq{}
	req.App = ctx.Query("app")
	if req.App == "" {
		return nil, fmt.Errorf("app is required")
	}
	req.FuzzyName = ctx.Query("fuzzyName")
	if req.FuzzyName == "" {
		return nil, fmt.Errorf("fuzzyName is required")
	}
	if idx := ctx.Query("pageIndex"); idx != "" {
		req.Index, _ = strconv.Atoi(idx)
	}
	if sz := ctx.Query("pageSize"); sz != "" {
		req.Size, _ = strconv.Atoi(sz)
	}
	return req, nil
}

type timerService interface {
	CreateTimer(ctx context.Context, timer *vo.Timer) (uint, error)
	DeleteTimer(ctx context.Context, app string, id uint) error
	UpdateTimer(ctx context.Context, timer *vo.Timer) error
	GetTimer(ctx context.Context, id uint) (*vo.Timer, error)
	EnableTimer(ctx context.Context, app string, id uint) error
	UnableTimer(ctx context.Context, app string, id uint) error
	GetAppTimers(ctx context.Context, req *vo.GetAppTimersReq) ([]*vo.Timer, int64, error)
	GetTimersByName(ctx context.Context, req *vo.GetTimersByNameReq) ([]*vo.Timer, int64, error)
}

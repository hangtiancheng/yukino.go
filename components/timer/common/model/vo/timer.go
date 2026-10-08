package vo

import (
	"encoding/json"
	"errors"

	"github.com/hangtiancheng/yukino.go/components/timer/common/consts"
	"github.com/hangtiancheng/yukino.go/components/timer/common/model/po"
)

type GetAppTimersReq struct {
	App string `form:"app" binding:"required"`
	PageLimiter
}

type GetTimersByNameReq struct {
	App       string `form:"app" binding:"required"`
	FuzzyName string `form:"fuzzyName" binding:"required"`
	PageLimiter
}

type GetTimersResp struct {
	CodeMsg
	Data  []*Timer `json:"data"`
	Total int64    `json:"total"`
}

func NewGetTimersResp(timers []*Timer, total int64, codeMsg CodeMsg) *GetTimersResp {
	return &GetTimersResp{
		Data:    timers,
		Total:   total,
		CodeMsg: codeMsg,
	}
}

type CreateTimerResp struct {
	CodeMsg
	ID uint `json:"id"`
}

func NewCreateTimerResp(id uint, codeMsg CodeMsg) *CreateTimerResp {
	return &CreateTimerResp{
		ID:      id,
		CodeMsg: codeMsg,
	}
}

type TimerReq struct {
	App string `form:"app" json:"app" binding:"required"`
	ID  uint   `form:"id" json:"id" binding:"required"`
}

type GetTimerResp struct {
	CodeMsg
	Data *Timer `json:"data"`
}

func NewGetTimerResp(timer *Timer, codeMsg CodeMsg) *GetTimerResp {
	return &GetTimerResp{
		CodeMsg: codeMsg,
		Data:    timer,
	}
}

type Timer struct {
	ID              uint               `json:"id,omitempty"`
	App             string             `json:"app,omitempty" binding:"required"`             // App name
	Name            string             `json:"name,omitempty" binding:"required"`            // Timer name
	Status          consts.TimerStatus `json:"status"`                                       // Timer status: 1=disabled, 2=enabled
	Cron            string             `json:"cron,omitempty" binding:"required"`            // Cron expression
	NotifyHTTPParam *NotifyHTTPParam   `json:"notifyHTTPParam,omitempty" binding:"required"` // HTTP callback parameters
}

type NotifyHTTPParam struct {
	Method string            `json:"method,omitempty" binding:"required"` // HTTP method: POST, GET, etc.
	URL    string            `json:"url,omitempty" binding:"required"`    // URL path
	Header map[string]string `json:"header,omitempty"`                    // Request headers
	Body   string            `json:"body,omitempty"`                      // Request body
}

func NewTimer(timer *po.Timer) (*Timer, error) {
	var param NotifyHTTPParam
	if err := json.Unmarshal([]byte(timer.NotifyHTTPParam), &param); err != nil {
		return nil, err
	}

	return &Timer{
		ID:              timer.ID,
		App:             timer.App,
		Name:            timer.Name,
		Status:          consts.TimerStatus(timer.Status),
		Cron:            timer.Cron,
		NotifyHTTPParam: &param,
	}, nil
}

func NewTimers(timers []*po.Timer) ([]*Timer, error) {
	vTimers := make([]*Timer, 0, len(timers))
	for _, timer := range timers {
		vTimer, err := NewTimer(timer)
		if err != nil {
			return nil, err
		}
		vTimers = append(vTimers, vTimer)
	}
	return vTimers, nil
}

func (t *Timer) Check() error {
	if t.NotifyHTTPParam == nil {
		return errors.New("empty notify http params")
	}
	return nil
}

func (t *Timer) ToPO() (*po.Timer, error) {
	if err := t.Check(); err != nil {
		return nil, err
	}

	param, err := json.Marshal(t.NotifyHTTPParam)
	if err != nil {
		return nil, err
	}

	timer := po.Timer{
		App:             t.App,
		Name:            t.Name,
		Status:          t.Status.ToInt(),
		Cron:            t.Cron,
		NotifyHTTPParam: string(param),
	}
	if timer.Status == 0 {
		timer.Status = consts.Unable.ToInt()
	}
	return &timer, nil
}

type MinuteBucket struct {
	Minute string
	Bucket int
}

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

package po

import (
	"time"

	"github.com/hangtiancheng/yukino.go/apps/timer_demo/common/consts"

	"gorm.io/gorm"
)

// Timer is a timer definition.
type Timer struct {
	gorm.Model
	App             string `gorm:"column:app;NOT NULL" json:"app,omitempty"`                             // App name
	Name            string `gorm:"column:name;NOT NULL" json:"name,omitempty"`                           // Timer name
	Status          int    `gorm:"column:status;NOT NULL" json:"status,omitempty"`                       // Timer status: 1=disabled, 2=enabled
	Cron            string `gorm:"column:cron;NOT NULL" json:"cron,omitempty"`                           // Cron expression
	NotifyHTTPParam string `gorm:"column:notify_http_param;NOT NULL" json:"notify_http_param,omitempty"` // HTTP callback parameters
}

func (t *Timer) TableName() string {
	return "timer"
}

func (t *Timer) BatchTasksFromTimer(executeTimes []time.Time) []*Task {
	tasks := make([]*Task, 0, len(executeTimes))
	for _, executeTime := range executeTimes {
		tasks = append(tasks, &Task{
			App:      t.App,
			TimerID:  t.ID,
			Status:   consts.NotRun.ToInt(),
			RunTimer: executeTime,
		})
	}
	return tasks
}

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

	"gorm.io/gorm"
)

// Task is an execution record for a timer run.
type Task struct {
	gorm.Model
	App      string    `gorm:"column:app;NOT NULL"`           // App name
	TimerID  uint      `gorm:"column:timer_id;NOT NULL"`      // Timer ID
	Output   string    `gorm:"column:output;default:null"`    // Execution result
	RunTimer time.Time `gorm:"column:run_timer;default:null"` // Execution time
	CostTime int       `gorm:"column:cost_time"`              // Execution cost in milliseconds
	Status   int       `gorm:"column:status;NOT NULL"`        // Current status
}

func (t *Task) TableName() string {
	return "task"
}

type MinuteTaskCnt struct {
	Minute string `gorm:"column:minute"`
	Cnt    int64  `gorm:"column:cnt"`
}

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

package consts

const (
	MinuteFormat = "2006-01-02 15:04"
	SecondFormat = "2006-01-02 15:04:00"
	HourFormat   = "2006-01-02 15"
	DayFormat    = "2006-01-02"
	// Default expiration: one day.
	BloomFilterKeyExpireSeconds = 24 * 60 * 60
)

type TaskStatus int

func (t TaskStatus) ToInt() int {
	return int(t)
}

type TimerStatus int

func (t TimerStatus) ToInt() int {
	return int(t)
}

const (
	NotRun  TaskStatus = 0
	Running TaskStatus = 1
	Succeed TaskStatus = 2
	Failed  TaskStatus = 3

	Unable TimerStatus = 1
	Enable TimerStatus = 2
)

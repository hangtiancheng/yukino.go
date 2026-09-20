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

package time_wheel

import (
	"container/list"
	"log/slog"
	"sync"
	"time"
)

type taskElement struct {
	task      func()
	pos       int
	cycle     int
	key       string
	executeAt time.Time
}

type TimeWheel struct {
	sync.Once
	interval     time.Duration
	ticker       *time.Ticker
	stopChan     chan struct{}
	addTaskCh    chan *taskElement
	removeTaskCh chan string
	slots        []*list.List
	curSlot      int
	keyToETask   map[string]*list.Element
}

func NewTimeWheel(slotNum int, interval time.Duration) *TimeWheel {
	if slotNum <= 0 {
		slotNum = 10
	}
	if interval <= 0 {
		interval = time.Second
	}

	t := TimeWheel{
		interval:     interval,
		ticker:       time.NewTicker(interval),
		stopChan:     make(chan struct{}),
		keyToETask:   make(map[string]*list.Element),
		slots:        make([]*list.List, 0, slotNum),
		addTaskCh:    make(chan *taskElement),
		removeTaskCh: make(chan string),
	}
	for i := 0; i < slotNum; i++ {
		t.slots = append(t.slots, list.New())
	}
	go t.run()
	return &t
}

func (t *TimeWheel) Stop() {
	t.Do(func() {
		t.ticker.Stop()
		close(t.stopChan)
	})
}

func (t *TimeWheel) AddTask(key string, task func(), executeAt time.Time) {
	select {
	case <-t.stopChan:
		// The wheel is stopped: drop the task instead of blocking forever.
		return
	case t.addTaskCh <- &taskElement{
		task:      task,
		key:       key,
		executeAt: executeAt,
	}:
	}
}

func (t *TimeWheel) RemoveTask(key string) {
	select {
	case <-t.stopChan:
		return
	case t.removeTaskCh <- key:
	}
}

func (t *TimeWheel) run() {
	defer func() {
		if err := recover(); err != nil {
			slog.Error("time wheel panicked", "panic", err)
		}
	}()

	for {
		select {
		case <-t.stopChan:
			return
		case <-t.ticker.C:
			t.tick()
		case task := <-t.addTaskCh:
			t.addTask(task)
		case removeKey := <-t.removeTaskCh:
			t.removeTask(removeKey)
		}
	}
}

func (t *TimeWheel) tick() {
	list := t.slots[t.curSlot]
	defer t.circularIncr()
	t.execute(list)
}

func (t *TimeWheel) execute(l *list.List) {
	// Iterate each list.
	for e := l.Front(); e != nil; {
		taskElement, _ := e.Value.(*taskElement)
		if taskElement.cycle > 0 {
			taskElement.cycle--
			e = e.Next()
			continue
		}

		// Execute the task.
		go func() {
			defer func() {
				if err := recover(); err != nil {
					slog.Error("task panicked", "panic", err)
				}
			}()
			taskElement.task()
		}()

		// After execution, remove the task from the wheel.
		next := e.Next()
		l.Remove(e)
		delete(t.keyToETask, taskElement.key)
		e = next
	}
}

func (t *TimeWheel) getPosAndCircle(executeAt time.Time) (int, int) {
	delay := int(time.Until(executeAt))
	// A past-due deadline must not produce a negative slot index.
	if delay < 0 {
		delay = 0
	}
	cycle := delay / (len(t.slots) * int(t.interval))
	pos := (t.curSlot + delay/int(t.interval)) % len(t.slots)
	return pos, cycle
}

func (t *TimeWheel) addTask(task *taskElement) {
	// Compute the slot here, inside the run goroutine, so curSlot is never
	// read concurrently with circularIncr advancing it.
	task.pos, task.cycle = t.getPosAndCircle(task.executeAt)
	list := t.slots[task.pos]
	if _, ok := t.keyToETask[task.key]; ok {
		t.removeTask(task.key)
	}
	eTask := list.PushBack(task)
	t.keyToETask[task.key] = eTask
}

func (t *TimeWheel) removeTask(key string) {
	eTask, ok := t.keyToETask[key]
	if !ok {
		return
	}
	delete(t.keyToETask, key)
	task, _ := eTask.Value.(*taskElement)
	_ = t.slots[task.pos].Remove(eTask)
}

func (t *TimeWheel) circularIncr() {
	t.curSlot = (t.curSlot + 1) % len(t.slots)
}

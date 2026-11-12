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
	nextTick     time.Time
	ticker       *time.Ticker
	stopChan     chan struct{}
	done         chan struct{}
	addTaskCh    chan *taskElement
	removeTaskCh chan string
	slots        []*list.List
	curSlot      int
	keyToETask   map[string]*list.Element
	taskWG       sync.WaitGroup
}

func NewTimeWheel(slotNum int, interval time.Duration) *TimeWheel {
	if slotNum <= 0 {
		slotNum = 10
	}
	if interval <= 0 {
		interval = time.Second
	}

	startedAt := time.Now()
	t := TimeWheel{
		interval:     interval,
		nextTick:     startedAt.Add(interval),
		ticker:       time.NewTicker(interval),
		stopChan:     make(chan struct{}),
		done:         make(chan struct{}),
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
	<-t.done
	t.taskWG.Wait()
}

func (t *TimeWheel) AddTask(key string, task func(), executeAt time.Time) {
	if key == "" || task == nil {
		return
	}
	select {
	case <-t.stopChan:
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
	defer close(t.done)
	defer func() {
		if err := recover(); err != nil {
			slog.Error("time wheel panicked", "panic", err)
		}
	}()

	for {
		select {
		case <-t.stopChan:
			return
		case tickAt := <-t.ticker.C:
			t.tick(tickAt)
		case task := <-t.addTaskCh:
			t.addTask(task)
		case removeKey := <-t.removeTaskCh:
			t.removeTask(removeKey)
		}
	}
}

func (t *TimeWheel) tick(tickAt time.Time) {
	list := t.slots[t.curSlot]
	defer t.circularIncr()
	t.execute(list)
	t.nextTick = tickAt.Add(t.interval)
}

func (t *TimeWheel) execute(l *list.List) {
	for e := l.Front(); e != nil; {
		taskElement, _ := e.Value.(*taskElement)
		if taskElement.cycle > 0 {
			taskElement.cycle--
			e = e.Next()
			continue
		}

		t.taskWG.Add(1)
		go func() {
			defer t.taskWG.Done()
			defer func() {
				if err := recover(); err != nil {
					slog.Error("task panicked", "panic", err)
				}
			}()
			taskElement.task()
		}()

		next := e.Next()
		l.Remove(e)
		delete(t.keyToETask, taskElement.key)
		e = next
	}
}

func (t *TimeWheel) getPosAndCircle(executeAt time.Time) (int, int) {
	ticks := int64(1)
	if delay := executeAt.Sub(t.nextTick); delay > 0 {
		ticks += int64((delay-1)/t.interval + 1)
	}
	advance := ticks - 1
	slotCount := int64(len(t.slots))
	pos := (t.curSlot + int(advance%slotCount)) % len(t.slots)
	cycle := int(advance / slotCount)
	return pos, cycle
}

func (t *TimeWheel) addTask(task *taskElement) {
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

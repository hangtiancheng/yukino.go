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

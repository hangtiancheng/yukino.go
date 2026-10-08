package conf

type SchedulerAppConf struct {
	SchedulersNum int `yaml:"schedulersNum"`
	WorkersNum    int `yaml:"workersNum"`
	// Adds one bucket for every 200 additional tasks beyond the default bucket count
	BucketsNum             int `yaml:"bucketsNum"`
	TryLockSeconds         int `yaml:"tryLockSeconds"`
	TryLockGapMilliSeconds int `yaml:"tryLockGapMilliSeconds"`
	SuccessExpireSeconds   int `yaml:"successExpireSeconds"`
}

var defaultSchedulerAppConfProvider *SchedulerAppConfProvider

type SchedulerAppConfProvider struct {
	conf *SchedulerAppConf
}

func NewSchedulerAppConfProvider(conf *SchedulerAppConf) *SchedulerAppConfProvider {
	return &SchedulerAppConfProvider{conf: conf}
}

func (s *SchedulerAppConfProvider) Get() *SchedulerAppConf {
	return s.conf
}

func DefaultSchedulerAppConfProvider() *SchedulerAppConfProvider {
	return defaultSchedulerAppConfProvider
}

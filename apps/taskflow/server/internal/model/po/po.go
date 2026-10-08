package po

import (
	"time"

	"gorm.io/gorm"
)

const (
	TaskTypeScheduled = "scheduled"
	TaskTypeCondition = "condition"
	TaskTypeManual    = "manual"

	StatusPending   = "pending"
	StatusReserved  = "reserved"
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"

	EventTypeMySQLInsert = "mysql_insert"
)

type ScheduledTask struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Name        string     `gorm:"size:128;uniqueIndex;not null" json:"name"`
	Description string     `gorm:"size:512" json:"description"`
	CronExpr    string     `gorm:"size:128;not null" json:"cron_expr"`
	Timezone    string     `gorm:"size:64" json:"timezone"`
	Prompt      string     `gorm:"type:mediumtext" json:"prompt"`
	Model       string     `gorm:"size:128" json:"model"`
	Enabled     bool       `gorm:"index;not null;default:false" json:"enabled"`
	LastFireAt  *time.Time `json:"last_fire_at"`
	NextFireAt  *time.Time `json:"next_fire_at"`
	CreatedAt   time.Time  `gorm:"type:datetime(6)" json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (ScheduledTask) TableName() string { return "scheduled_tasks" }

func (t *ScheduledTask) BeforeCreate(tx *gorm.DB) error {
	return databaseCreatedAt(tx, &t.CreatedAt)
}

type ConditionTask struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:128;uniqueIndex;not null" json:"name"`
	Description string    `gorm:"size:512" json:"description"`
	EventType   string    `gorm:"size:64;not null;default:'mysql_insert'" json:"event_type"`
	WatchTable  string    `gorm:"column:table_name;size:128;not null;index" json:"table_name"`
	Prompt      string    `gorm:"type:mediumtext" json:"prompt"`
	Model       string    `gorm:"size:128" json:"model"`
	Enabled     bool      `gorm:"index;not null;default:false" json:"enabled"`
	CreatedAt   time.Time `gorm:"type:datetime(6)" json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ConditionTask) TableName() string { return "condition_tasks" }

func (t *ConditionTask) BeforeCreate(tx *gorm.DB) error {
	return databaseCreatedAt(tx, &t.CreatedAt)
}

// Subscription ordering uses the same clock and precision as SQL capture.
// Host clock skew or millisecond rounding must not drop a newly inserted row.
func databaseCreatedAt(tx *gorm.DB, timestamp *time.Time) error {
	if !timestamp.IsZero() {
		return nil
	}
	var clock struct{ Now time.Time }
	if err := tx.Raw("SELECT UTC_TIMESTAMP(6) AS now").Scan(&clock).Error; err != nil {
		return err
	}
	*timestamp = clock.Now
	return nil
}

type Execution struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	TaskType       string     `gorm:"size:32;index;not null" json:"task_type"`
	TaskID         uint       `gorm:"index" json:"task_id"`
	TaskName       string     `gorm:"size:128" json:"task_name"`
	FireKey        string     `gorm:"size:191;uniqueIndex;not null" json:"fire_key"`
	TxID           string     `gorm:"size:64;index" json:"tx_id"`
	Status         string     `gorm:"size:32;index;not null" json:"status"`
	TriggerInfo    string     `gorm:"type:mediumtext" json:"trigger_info"`
	PromptSnapshot string     `gorm:"type:mediumtext" json:"-"`
	ModelSnapshot  string     `gorm:"size:128" json:"-"`
	ReportPath     string     `gorm:"size:256" json:"report_path"`
	ReportExcerpt  string     `gorm:"type:text" json:"report_excerpt"`
	ReportBody     string     `gorm:"type:mediumtext" json:"-"`
	Error          string     `gorm:"type:text" json:"error"`
	TraceID        string     `gorm:"size:64;index" json:"trace_id"`
	NodeID         string     `gorm:"size:128" json:"node_id"`
	FireAt         time.Time  `gorm:"index;not null" json:"fire_at"`
	ClaimedAt      *time.Time `json:"claimed_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	ToolCalls      int        `json:"tool_calls"`
	TokensPrompt   int64      `json:"tokens_prompt"`
	TokensOutput   int64      `json:"tokens_output"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Execution) TableName() string { return "executions" }

type RiskRecord struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Title     string    `gorm:"size:256" json:"title"`
	Content   string    `gorm:"type:text" json:"content"`
	Source    string    `gorm:"size:128" json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

func (RiskRecord) TableName() string { return "risk_records" }

type DeadLetter struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Topic     string    `gorm:"size:128;index" json:"topic"`
	MsgID     string    `gorm:"size:128" json:"msg_id"`
	MsgKey    string    `gorm:"size:191" json:"msg_key"`
	Val       string    `gorm:"type:mediumtext" json:"val"`
	Reason    string    `gorm:"type:text" json:"reason"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (DeadLetter) TableName() string { return "mq_dead_letters" }

type TXRecord struct {
	ID                   uint   `gorm:"primaryKey"`
	Status               string `gorm:"size:32;index;not null"`
	ComponentTryStatuses string `gorm:"type:text"`
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (TXRecord) TableName() string { return "tcc_tx_records" }

// ChangeEvent is inserted by an AFTER INSERT/DELETE trigger in the same
// transaction as the business mutation. UUID identity survives ID reuse.
type ChangeEvent struct {
	ID          uint64     `gorm:"primaryKey" json:"id"`
	EventKey    string     `gorm:"size:64;uniqueIndex;not null" json:"event_key"`
	SourceTable string     `gorm:"column:table_name;size:64;index:idx_change_window,priority:1;not null" json:"table_name"`
	Operation   string     `gorm:"size:16;not null" json:"operation"`
	RecordJSON  string     `gorm:"type:json;not null" json:"record_json"`
	OccurredAt  time.Time  `gorm:"type:datetime(6);index:idx_change_window,priority:2;not null" json:"occurred_at"`
	ProcessedAt *time.Time `gorm:"index" json:"processed_at"`
}

func (ChangeEvent) TableName() string { return "taskflow_changes" }

// Outbox bridges MySQL commit and Redis publication. A crash after XADD but
// before commit redelivers the same key, which the execution CAS suppresses.
type Outbox struct {
	ID          uint64     `gorm:"primaryKey" json:"id"`
	EventKey    string     `gorm:"size:191;uniqueIndex;not null" json:"event_key"`
	Topic       string     `gorm:"size:128;not null" json:"topic"`
	Payload     string     `gorm:"type:mediumtext;not null" json:"-"`
	PublishedAt *time.Time `gorm:"index" json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (Outbox) TableName() string { return "taskflow_outbox" }

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&ScheduledTask{},
		&ConditionTask{},
		&Execution{},
		&RiskRecord{},
		&DeadLetter{},
		&TXRecord{},
		&ChangeEvent{},
		&Outbox{},
	)
}

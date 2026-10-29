package model

// CGAsyncRequest is the GORM model of the cg_async_request table.
//
// State machine: Pending → InProgress → Completed/Failed.
//
//	con_reader writes the Pending row → a Worker CAS-claims it to InProgress → execution flips it to Completed/Failed.
//
// It is also the data source of zombie-recovery scans (ListZombies queries InProgress records with expired heartbeat_at).
type CGAsyncRequest struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// Async-request business primary key (snowflake ID string), globally unique
	AsyncRequestID string `gorm:"column:async_request_id;uniqueIndex;size:64;not null"`
	// Associated with cg_message_record.message_id; one async request maps to one message record
	MessageID string `gorm:"column:message_id;index;size:64;not null"`
	// Session ID, associated with cg_session.session_id
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// User ID
	UserID int64 `gorm:"column:user_id;not null"`
	// Agent ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// Serialized SupervisorBusinessParams JSON; the Worker deserializes it to rebuild the supervisor context
	Query string `gorm:"column:query;type:mediumtext;not null"`
	// Idempotency dedup key for a 5s window (MD5(sessionID+msgID+query)), preventing rapid duplicate clicks
	DedupKey string `gorm:"column:dedup_key;index;size:128;not null;default:''"`
	// Worker ID currently holding execution (set on CAS claim), used for ownership validation
	WorkerID string `gorm:"column:worker_id;size:64;not null;default:''"`
	// Status: 0=Pending, 1=InProgress, 2=Completed, 3=Failed
	Status int8 `gorm:"column:status;not null;default:0;index:idx_status_heartbeat,priority:1"`
	// Attempt count (atomically +1 on each Takeover CAS); beyond MaxAsyncAttempt it goes straight to Failed
	AttemptCount int `gorm:"column:attempt_count;not null;default:0"`
	// Latest heartbeat time (millisecond timestamp), refreshed by the Worker every 5s; zombie-scan criterion: older than 15s counts as dead
	HeartbeatAt int64 `gorm:"column:heartbeat_at;not null;default:0;index:idx_status_heartbeat,priority:2"`
	// Last error message (records the reason when the terminal state is Failed)
	LastError string `gorm:"column:last_error;size:1024;not null;default:''"`
	// Request log trace ID
	LogID string `gorm:"column:log_id;size:64;not null;default:''"`
	// Shard key (fixed to 0 in the replica environment)
	ShardingKey int32 `gorm:"column:sharding_key;not null;default:0"`
	// Creation time (millisecond timestamp)
	CreateTime int64 `gorm:"column:create_time;not null"`
	// Update time (millisecond timestamp)
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

func (CGAsyncRequest) TableName() string { return "cg_async_request" }

// cg_async_request status constants.
const (
	AsyncStatusPending    int8 = 0 // persisted, waiting for a Worker to consume
	AsyncStatusInProgress int8 = 1 // claimed by a Worker, executing
	AsyncStatusCompleted  int8 = 2 // executed successfully
	AsyncStatusFailed     int8 = 3 // execution failed (terminal)
)

// MaxAsyncAttempt is the maximum number of attempts for an async request.
const MaxAsyncAttempt = 3

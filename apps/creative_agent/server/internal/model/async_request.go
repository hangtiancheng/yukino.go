package model

// CGAsyncRequest 是 cg_async_request 表的 GORM 模型。
//
// 状态机：Pending → InProgress → Completed/Failed。
//
//	con_reader 写入 Pending 行 → Worker CAS 抢占翻 InProgress → 执行完翻 Completed/Failed。
//
// 同时也是僵尸恢复扫描的数据源（ListZombies 查 heartbeat_at 过期的 InProgress 记录）。
type CGAsyncRequest struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// 异步请求业务主键（雪花 ID 字符串），全局唯一
	AsyncRequestID string `gorm:"column:async_request_id;uniqueIndex;size:64;not null"`
	// 关联 cg_message_record.message_id，一个异步请求对应一条消息记录
	MessageID string `gorm:"column:message_id;index;size:64;not null"`
	// 会话 ID，关联 cg_session.session_id
	SessionID string `gorm:"column:session_id;index;size:64;not null"`
	// 用户 ID
	UserID int64 `gorm:"column:user_id;not null"`
	// 智能体 ID
	AgentID string `gorm:"column:agent_id;size:64;not null"`
	// 序列化的 SupervisorBusinessParams JSON，Worker 反序列化后重建 supervisor 上下文
	Query string `gorm:"column:query;type:mediumtext;not null"`
	// 5s 窗口幂等去重键（MD5(sessionID+msgID+query)），防用户快速重复点击
	DedupKey string `gorm:"column:dedup_key;index;size:128;not null;default:''"`
	// 当前持有执行的 worker ID（CAS 抢占时设置），用于归属校验
	WorkerID string `gorm:"column:worker_id;size:64;not null;default:''"`
	// 状态：0=Pending, 1=InProgress, 2=Completed, 3=Failed
	Status int8 `gorm:"column:status;not null;default:0;index:idx_status_heartbeat,priority:1"`
	// 尝试次数（每次 Takeover CAS 时原子 +1），超 MaxAsyncAttempt 直接 Failed
	AttemptCount int `gorm:"column:attempt_count;not null;default:0"`
	// 最近心跳时间（毫秒时间戳），Worker 每 5s 刷新；僵尸扫描判据：超过 15s 认定为死亡
	HeartbeatAt int64 `gorm:"column:heartbeat_at;not null;default:0;index:idx_status_heartbeat,priority:2"`
	// 最后一次错误信息（终态为 Failed 时记录原因）
	LastError string `gorm:"column:last_error;size:1024;not null;default:''"`
	// 请求日志追踪 ID
	LogID string `gorm:"column:log_id;size:64;not null;default:''"`
	// 分片键（复刻环境固定为 0）
	ShardingKey int32 `gorm:"column:sharding_key;not null;default:0"`
	// 创建时间（毫秒时间戳）
	CreateTime int64 `gorm:"column:create_time;not null"`
	// 更新时间（毫秒时间戳）
	UpdateTime int64 `gorm:"column:update_time;not null"`
}

func (CGAsyncRequest) TableName() string { return "cg_async_request" }

// cg_async_request 状态常量。
const (
	AsyncStatusPending    int8 = 0 // 已落库，等待 Worker 消费
	AsyncStatusInProgress int8 = 1 // 被某 Worker 抢占，执行中
	AsyncStatusCompleted  int8 = 2 // 执行成功完成
	AsyncStatusFailed     int8 = 3 // 执行失败（终态）
)

// MaxAsyncAttempt 是异步请求最大尝试次数。
const MaxAsyncAttempt = 3

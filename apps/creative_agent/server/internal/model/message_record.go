package model

// CGMessageRecord 是 cg_message_record 表的 GORM 模型。
// 采用"先建后改"模型：con_reader 建行（status=InProgress，只填 QueryInfo），
// con_writer 逐帧回填 ChatResponse。
type CGMessageRecord struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	MessageID   string `gorm:"column:message_id;uniqueIndex;size:64;not null"`
	UserID      int64  `gorm:"column:user_id;not null"`
	AgentID     string `gorm:"column:agent_id;size:64;not null"`
	SessionID   string `gorm:"column:session_id;index;size:64;not null"`
	ChatText    string `gorm:"column:chat_text;type:mediumtext;not null"`
	Status      int8   `gorm:"column:status;not null;default:2"`
	IsRisk      int8   `gorm:"column:is_risk;not null;default:0"`
	SeqMax      int64  `gorm:"column:seq_max;not null;default:0"`
	LogID       string `gorm:"column:log_id;size:64;not null;default:''"`
	MessageType int32  `gorm:"column:message_type;not null;default:0"`
	Pd          string `gorm:"column:pd;size:32;not null;default:''"`
	ShardingKey int32  `gorm:"column:sharding_key;not null;default:0"`
	CreateTime  int64  `gorm:"column:create_time;not null"`
	UpdateTime  int64  `gorm:"column:update_time;not null"`
}

func (CGMessageRecord) TableName() string { return "cg_message_record" }

// Message 状态常量。
const (
	MessageStatusNormal     int8 = 1 // 终态
	MessageStatusInProgress int8 = 2 // 进行中
	IsRiskNo                int8 = 0
	IsRiskYes               int8 = 1
)

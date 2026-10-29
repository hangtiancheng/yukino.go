package model

// CGSession is the GORM model of the cg_session table.
// One session (conversation window) maps to many message_record rows.
type CGSession struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	SessionID        string `gorm:"column:session_id;uniqueIndex;size:64;not null"`
	UserID           int64  `gorm:"column:user_id;index;not null"`
	AgentID          string `gorm:"column:agent_id;size:64;not null"`
	Status           int8   `gorm:"column:status;not null;default:0"`
	Pd               string `gorm:"column:pd;size:32;not null;default:''"`
	SessionName      string `gorm:"column:session_name;size:256;not null;default:'默认会话'"`
	RecordUpdateTime int64  `gorm:"column:record_update_time;not null;default:0"`
	ShardingKey      int32  `gorm:"column:sharding_key;not null;default:0"`
	CreateTime       int64  `gorm:"column:create_time;not null"`
	UpdateTime       int64  `gorm:"column:update_time;not null"`
}

func (CGSession) TableName() string { return "cg_session" }

// Session status constants.
const (
	SessionStatusInit      int8 = 0 // initial state
	SessionStatusDialogued int8 = 1 // has conversation
)

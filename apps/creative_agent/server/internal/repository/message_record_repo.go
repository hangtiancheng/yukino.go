package repository

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/creative_agent/server/internal/model"

	"gorm.io/gorm"
)

// ErrMessageRecordNotFound 表示未找到对应 message_record 记录。
var ErrMessageRecordNotFound = errors.New("message record not found")

// MessageRecordRepository 封装 cg_message_record 表的数据访问。
// 采用 "先建后改" 模型：con_reader 建行，con_writer 逐帧回填。
type MessageRecordRepository interface {
	Insert(ctx context.Context, m *model.CGMessageRecord) error
	GetByMessageID(ctx context.Context, messageID string) (*model.CGMessageRecord, error)
	UpdateByMessageID(ctx context.Context, messageID string, assigns map[string]any) error
	// ReadModifyWriteInTx 在事务内执行原子「读-改-写」。
	ReadModifyWriteInTx(ctx context.Context, messageID string, fn func(*model.CGMessageRecord) (map[string]any, error)) error
}

type messageRecordRepository struct {
	db *gorm.DB
}

// NewMessageRecordRepository 基于 gorm.DB 创建 MessageRecordRepository。
func NewMessageRecordRepository(db *gorm.DB) MessageRecordRepository {
	return &messageRecordRepository{db: db}
}

func (r *messageRecordRepository) Insert(ctx context.Context, m *model.CGMessageRecord) error {
	now := time.Now().UnixMilli()
	if m.CreateTime == 0 {
		m.CreateTime = now
	}
	if m.UpdateTime == 0 {
		m.UpdateTime = now
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *messageRecordRepository) GetByMessageID(ctx context.Context, messageID string) (*model.CGMessageRecord, error) {
	var rec model.CGMessageRecord
	err := r.db.WithContext(ctx).Where("message_id = ?", messageID).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrMessageRecordNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *messageRecordRepository) UpdateByMessageID(ctx context.Context, messageID string, assigns map[string]any) error {
	assigns["update_time"] = time.Now().UnixMilli()
	return r.db.WithContext(ctx).Model(&model.CGMessageRecord{}).
		Where("message_id = ?", messageID).
		Updates(assigns).Error
}

func (r *messageRecordRepository) ReadModifyWriteInTx(ctx context.Context, messageID string, fn func(*model.CGMessageRecord) (map[string]any, error)) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rec model.CGMessageRecord
		if err := tx.Where("message_id = ?", messageID).First(&rec).Error; err != nil {
			return err
		}
		assigns, err := fn(&rec)
		if err != nil {
			return err
		}
		assigns["update_time"] = time.Now().UnixMilli()
		return tx.Model(&rec).Where("message_id = ?", messageID).Updates(assigns).Error
	})
}

package engine

import (
	"context"
	"log/slog"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/dao"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/obsx"
	mqredis "github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

type DeadLetterMailbox struct {
	dao   *dao.DAO
	topic string
}

func NewDeadLetterMailbox(d *dao.DAO) *DeadLetterMailbox {
	return &DeadLetterMailbox{dao: d}
}

func (m *DeadLetterMailbox) ForTopic(topic string) *DeadLetterMailbox {
	return &DeadLetterMailbox{dao: m.dao, topic: topic}
}

func (m *DeadLetterMailbox) Deliver(ctx context.Context, msg *mqredis.MsgEntity) error {
	slog.Error("delivering message to dead letter store", "topic", m.topic, "msg_id", msg.MsgID)

	if err := m.dao.CreateDeadLetter(ctx, &po.DeadLetter{
		Topic:  m.topic,
		MsgID:  msg.MsgID,
		MsgKey: msg.Key,
		Val:    msg.Val,
		Reason: "retry limit exceeded",
	}); err != nil {
		return err
	}

	obsx.CaptureError(ctx, errDeadLetter{topic: m.topic, msgID: msg.MsgID, val: msg.Val}, map[string]string{
		"topic": m.topic,
	})
	return nil
}

type errDeadLetter struct {
	topic string
	msgID string
	val   string
}

func (e errDeadLetter) Error() string {
	return "dead letter on topic " + e.topic + " msg " + e.msgID + ": " + e.val
}

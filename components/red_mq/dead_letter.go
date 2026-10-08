package red_mq

import (
	"context"

	"github.com/hangtiancheng/yukino.go/components/red_mq/log"
	"github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

// DeadLetterMailbox receives messages that have exceeded the retry limit.
type DeadLetterMailbox interface {
	Deliver(ctx context.Context, msg *redis.MsgEntity) error
}

// DeadLetterLogger is the default DeadLetterMailbox. It simply logs the message.
type DeadLetterLogger struct{}

func NewDeadLetterLogger() *DeadLetterLogger {
	return &DeadLetterLogger{}
}

func (d *DeadLetterLogger) Deliver(ctx context.Context, msg *redis.MsgEntity) error {
	log.ErrorContextf(ctx, "msg fail exceed retry limit, msg id: %s", msg.MsgID)
	return nil
}

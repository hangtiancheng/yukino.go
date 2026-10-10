package red_mq

import (
	"context"

	"github.com/hangtiancheng/yukino.go/components/red_mq/log"
	"github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

type DeadLetterMailbox interface {
	Deliver(ctx context.Context, msg *redis.MsgEntity) error
}

type DeadLetterLogger struct{}

func NewDeadLetterLogger() *DeadLetterLogger {
	return &DeadLetterLogger{}
}

func (d *DeadLetterLogger) Deliver(ctx context.Context, msg *redis.MsgEntity) error {
	log.ErrorContextf(ctx, "msg fail exceed retry limit, msg id: %s", msg.MsgID)
	return nil
}

package redis_mq

import (
	"context"

	"github.com/hangtiancheng/yukino.go/components/redis_mq/internal/log"
)

type DeadLetterMailbox interface {
	Deliver(ctx context.Context, msg *MsgEntity) error
}

type DeadLetterLogger struct{}

func NewDeadLetterLogger() *DeadLetterLogger {
	return &DeadLetterLogger{}
}

func (d *DeadLetterLogger) Deliver(ctx context.Context, msg *MsgEntity) error {
	log.ErrorContextf(ctx, "msg fail exceed retry limit, msg id: %s", msg.MsgID)
	return nil
}

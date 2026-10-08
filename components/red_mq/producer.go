package red_mq

import (
	"context"

	"github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

type Producer struct {
	client *redis.Client
	opts   *ProducerOptions
}

func NewProducer(client *redis.Client, opts ...ProducerOption) *Producer {
	p := Producer{
		client: client,
		opts:   &ProducerOptions{},
	}

	for _, opt := range opts {
		opt(p.opts)
	}

	repairProducer(p.opts)

	return &p
}

// SendMsg produces a message to the given topic.
func (p *Producer) SendMsg(ctx context.Context, topic, key, val string) (string, error) {
	return p.client.XADD(ctx, topic, p.opts.msgQueueLen, key, val)
}

package example

import (
	"context"
	"testing"

	"github.com/hangtiancheng/yukino.go/components/red_mq"
	"github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

func Test_Producer(t *testing.T) {
	skipWithoutRedis(t)
	client := redis.NewClient(network, address, password)
	defer client.Close()
	// Keep at most 10 messages in the stream.
	producer := red_mq.NewProducer(client, red_mq.WithMsgQueueLen(10))
	ctx := context.Background()
	msgID, err := producer.SendMsg(ctx, topic, "test_kk", "test_vv")
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(msgID)
}

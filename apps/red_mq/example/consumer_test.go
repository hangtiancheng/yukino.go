// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package example

import (
	"context"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/red_mq"
	"github.com/hangtiancheng/yukino.go/apps/red_mq/redis"
)

const (
	network       = "tcp"
	address       = "please fill in redis address"
	password      = "please fill in redis password"
	topic         = "please fill in topic name"
	consumerGroup = "please fill in consumer group name"
	consumerID    = "please fill in consumer name"
)

// DemoDeadLetterMailbox is a custom dead-letter mailbox.
type DemoDeadLetterMailbox struct {
	do func(msg *redis.MsgEntity)
}

func NewDemoDeadLetterMailbox(do func(msg *redis.MsgEntity)) *DemoDeadLetterMailbox {
	return &DemoDeadLetterMailbox{
		do: do,
	}
}

// Deliver handles a dead-letter message.
func (d *DemoDeadLetterMailbox) Deliver(ctx context.Context, msg *redis.MsgEntity) error {
	d.do(msg)
	return nil
}

// skipWithoutRedis skips the integration tests until the connection
// constants above are filled in with a real redis.
func skipWithoutRedis(t *testing.T) {
	t.Helper()
	if address == "please fill in redis address" {
		t.Skip("redis address not configured, fill in the placeholders to run the integration tests")
	}
}

func Test_Consumer(t *testing.T) {
	skipWithoutRedis(t)
	client := redis.NewClient(network, address, password)
	defer client.Close()

	// Message handler.
	callbackFunc := func(ctx context.Context, msg *redis.MsgEntity) error {
		t.Logf("receive msg, msg id: %s, msg key: %s, msg val: %s", msg.MsgID, msg.Key, msg.Val)
		return nil
	}

	// Custom dead-letter mailbox.
	demoDeadLetterMailbox := NewDemoDeadLetterMailbox(func(msg *redis.MsgEntity) {
		t.Logf("receive dead letter, msg id: %s, msg key: %s, msg val: %s", msg.MsgID, msg.Key, msg.Val)
	})

	// Build and start the consumer.
	consumer, err := red_mq.NewConsumer(client, topic, consumerGroup, consumerID, callbackFunc,
		// Max 2 retries per message.
		red_mq.WithMaxRetryLimit(2),
		// 2s receive timeout per poll.
		red_mq.WithReceiveTimeout(2*time.Second),
		// Inject the custom dead-letter mailbox.
		red_mq.WithDeadLetterMailbox(demoDeadLetterMailbox))
	if err != nil {
		t.Error(err)
		return
	}
	defer consumer.Stop()

	// Exit the test after 10 seconds.
	<-time.After(10 * time.Second)
}

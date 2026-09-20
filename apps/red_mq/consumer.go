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

package red_mq

import (
	"context"
	"errors"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/red_mq/log"
	"github.com/hangtiancheng/yukino.go/apps/red_mq/redis"
)

// MsgCallback is invoked for each received message.
type MsgCallback func(ctx context.Context, msg *redis.MsgEntity) error

// Consumer reads messages from a redis stream consumer group.
type Consumer struct {
	// Lifecycle management.
	ctx  context.Context
	stop context.CancelFunc

	// callbackFunc is the user-supplied handler invoked for each message.
	callbackFunc MsgCallback

	// client is the redis client backing the message queue.
	client *redis.Client

	// topic is the stream to consume from.
	topic string
	// groupID is the consumer group.
	groupID string
	// consumerID is the current node's consumer id.
	consumerID string

	// failureCnts tracks the cumulative failure count per message.
	failureCnts map[redis.MsgEntity]int

	// opts holds user-supplied configuration.
	opts *ConsumerOptions
}

func NewConsumer(client *redis.Client, topic, groupID, consumerID string, callbackFunc MsgCallback, opts ...ConsumerOption) (*Consumer, error) {

	ctx, stop := context.WithCancel(context.Background())
	c := Consumer{
		client:       client,
		ctx:          ctx,
		stop:         stop,
		callbackFunc: callbackFunc,
		topic:        topic,
		groupID:      groupID,
		consumerID:   consumerID,

		opts: &ConsumerOptions{},

		failureCnts: make(map[redis.MsgEntity]int),
	}

	if err := c.checkParam(); err != nil {
		// Release the context created above, otherwise it leaks.
		stop()
		return nil, err
	}

	for _, opt := range opts {
		opt(c.opts)
	}

	repairConsumer(c.opts)

	go c.run()
	return &c, nil
}

func (c *Consumer) checkParam() error {
	if c.callbackFunc == nil {
		return errors.New("callback function can't be empty")
	}

	if c.client == nil {
		return errors.New("redis client can't be empty")
	}

	if c.topic == "" || c.consumerID == "" || c.groupID == "" {
		return errors.New("topic | group_id | consumer_id can't be empty")
	}

	return nil
}

// Stop signals the consumer to exit.
func (c *Consumer) Stop() {
	c.stop()
}

// retryBackoff is the pause between receive attempts after an error, so a
// persistently failing redis does not turn run into a hot error loop.
const retryBackoff = time.Second

// backoff waits before the next receive attempt. It reports false when the
// consumer was stopped while waiting.
func (c *Consumer) backoff() bool {
	select {
	case <-c.ctx.Done():
		return false
	case <-time.After(retryBackoff):
		return true
	}
}

// run is the consumer main loop.
func (c *Consumer) run() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Receive new messages.
		msgs, err := c.receive()
		if err != nil {
			log.ErrorContextf(c.ctx, "receive msg failed, err: %v", err)
			if !c.backoff() {
				return
			}
			continue
		}

		ctx, cancel := context.WithTimeout(c.ctx, c.opts.handleMsgsTimeout)
		c.handlerMsgs(ctx, msgs)
		cancel()

		// Deliver dead letters.
		ctx, cancel = context.WithTimeout(c.ctx, c.opts.deadLetterDeliverTimeout)
		c.deliverDeadLetter(ctx)
		cancel()

		// Receive and process pending messages.
		pendingMsgs, err := c.receivePending()
		if err != nil {
			log.ErrorContextf(c.ctx, "pending msg received failed, err: %v", err)
			if !c.backoff() {
				return
			}
			continue
		}

		ctx, cancel = context.WithTimeout(c.ctx, c.opts.handleMsgsTimeout)
		c.handlerMsgs(ctx, pendingMsgs)
		cancel()
	}
}

func (c *Consumer) receive() ([]*redis.MsgEntity, error) {
	msgs, err := c.client.XReadGroup(c.ctx, c.groupID, c.consumerID, c.topic, int(c.opts.receiveTimeout.Milliseconds()))
	if err != nil && !errors.Is(err, redis.ErrNoMsg) {
		return nil, err
	}

	return msgs, nil
}

func (c *Consumer) receivePending() ([]*redis.MsgEntity, error) {
	pendingMsgs, err := c.client.XReadGroupPending(c.ctx, c.groupID, c.consumerID, c.topic)
	if err != nil && !errors.Is(err, redis.ErrNoMsg) {
		return nil, err
	}

	return pendingMsgs, nil
}

func (c *Consumer) handlerMsgs(ctx context.Context, msgs []*redis.MsgEntity) {
	for _, msg := range msgs {
		if err := c.callbackFunc(ctx, msg); err != nil {
			// Increment the failure counter.
			c.failureCnts[*msg]++
			continue
		}

		// Callback succeeded: ack the message.
		if err := c.client.XACK(ctx, c.topic, c.groupID, msg.MsgID); err != nil {
			log.ErrorContextf(ctx, "msg ack failed, msg id: %s, err: %v", msg.MsgID, err)
			continue
		}

		delete(c.failureCnts, *msg)
	}
}

func (c *Consumer) deliverDeadLetter(ctx context.Context) {
	// Messages that have failed enough times are delivered to the dead-letter mailbox, then acked.
	for msg, failureCnt := range c.failureCnts {
		if failureCnt < c.opts.maxRetryLimit {
			continue
		}

		// Deliver to the dead-letter mailbox.
		if err := c.opts.deadLetterMailbox.Deliver(ctx, &msg); err != nil {
			log.ErrorContextf(c.ctx, "dead letter deliver failed, msg id: %s, err: %v", msg.MsgID, err)
			// Keep the message around, it must not be acked before it
			// reached the dead-letter mailbox.
			continue
		}

		// Ack the message.
		if err := c.client.XACK(ctx, c.topic, c.groupID, msg.MsgID); err != nil {
			log.ErrorContextf(c.ctx, "msg ack failed, msg id: %s, err: %v", msg.MsgID, err)
			continue
		}

		// Remove acked messages from the failure map.
		delete(c.failureCnts, msg)
	}
}

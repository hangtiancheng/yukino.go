package red_mq

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hangtiancheng/yukino.go/components/red_mq/log"
	"github.com/hangtiancheng/yukino.go/components/red_mq/redis"
)

type MsgCallback func(ctx context.Context, msg *redis.MsgEntity) error

type Consumer struct {
	ctx  context.Context
	stop context.CancelFunc
	done chan struct{}

	callbackFunc MsgCallback

	client *redis.Client

	topic      string
	groupID    string
	consumerID string

	failureCnts map[redis.MsgEntity]int

	opts *ConsumerOptions
}

func NewConsumer(client *redis.Client, topic, groupID, consumerID string, callbackFunc MsgCallback, opts ...ConsumerOption) (*Consumer, error) {

	ctx, stop := context.WithCancel(context.Background())
	c := Consumer{
		client:       client,
		ctx:          ctx,
		stop:         stop,
		done:         make(chan struct{}),
		callbackFunc: callbackFunc,
		topic:        topic,
		groupID:      groupID,
		consumerID:   consumerID,

		opts: &ConsumerOptions{},

		failureCnts: make(map[redis.MsgEntity]int),
	}

	if err := c.checkParam(); err != nil {
		stop()
		return nil, err
	}

	for _, opt := range opts {
		opt(c.opts)
	}

	repairConsumer(c.opts)

	if err := c.client.XGroupCreate(ctx, topic, groupID); err != nil {
		stop()
		return nil, err
	}

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

func (c *Consumer) Stop() {
	c.stop()
	<-c.done
}

const retryBackoff = time.Second

func (c *Consumer) backoff() bool {
	select {
	case <-c.ctx.Done():
		return false
	case <-time.After(retryBackoff):
		return true
	}
}

func (c *Consumer) run() {
	defer close(c.done)
	claimCursor := "0-0"
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		if c.opts.claimIdle > 0 {
			messages, next, err := c.client.XAutoClaim(c.ctx, c.topic, c.groupID, c.consumerID, claimCursor, c.opts.claimIdle)
			if err == nil {
				claimCursor = next
				ctx, cancel := context.WithTimeout(c.ctx, c.opts.handleMsgsTimeout)
				c.handlerMsgs(ctx, messages)
				cancel()
			}
		}
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

		ctx, cancel = context.WithTimeout(c.ctx, c.opts.deadLetterDeliverTimeout)
		c.deliverDeadLetter(ctx)
		cancel()

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
		if err := c.invoke(ctx, msg); err != nil {
			c.failureCnts[*msg]++
			continue
		}

		if err := c.client.XACK(ctx, c.topic, c.groupID, msg.MsgID); err != nil {
			log.ErrorContextf(ctx, "msg ack failed, msg id: %s, err: %v", msg.MsgID, err)
			continue
		}

		delete(c.failureCnts, *msg)
	}
}

func (c *Consumer) deliverDeadLetter(ctx context.Context) {
	for msg, failureCnt := range c.failureCnts {
		if failureCnt < c.opts.maxRetryLimit {
			continue
		}

		if err := c.opts.deadLetterMailbox.Deliver(ctx, &msg); err != nil {
			log.ErrorContextf(c.ctx, "dead letter deliver failed, msg id: %s, err: %v", msg.MsgID, err)
			continue
		}

		if err := c.client.XACK(ctx, c.topic, c.groupID, msg.MsgID); err != nil {
			log.ErrorContextf(c.ctx, "msg ack failed, msg id: %s, err: %v", msg.MsgID, err)
			continue
		}

		delete(c.failureCnts, msg)
	}
}

func (c *Consumer) invoke(ctx context.Context, msg *redis.MsgEntity) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("message handler panicked: %v", recovered)
		}
	}()
	return c.callbackFunc(ctx, msg)
}

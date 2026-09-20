package red_mq

import (
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/red_mq/redis"
)

func TestRepairProducer(t *testing.T) {
	opts := &ProducerOptions{}
	repairProducer(opts)
	if opts.msgQueueLen != 500 {
		t.Fatalf("default msgQueueLen = %d, want 500", opts.msgQueueLen)
	}

	opts = &ProducerOptions{msgQueueLen: -1}
	repairProducer(opts)
	if opts.msgQueueLen != 500 {
		t.Fatalf("negative msgQueueLen = %d, want 500", opts.msgQueueLen)
	}

	opts = &ProducerOptions{msgQueueLen: 7}
	repairProducer(opts)
	if opts.msgQueueLen != 7 {
		t.Fatalf("explicit msgQueueLen = %d, want 7", opts.msgQueueLen)
	}
}

func TestRepairConsumerDefaults(t *testing.T) {
	opts := &ConsumerOptions{}
	repairConsumer(opts)

	if opts.receiveTimeout != 2*time.Second {
		t.Fatalf("default receiveTimeout = %v, want 2s", opts.receiveTimeout)
	}
	if opts.maxRetryLimit != 3 {
		t.Fatalf("default maxRetryLimit = %d, want 3", opts.maxRetryLimit)
	}
	if opts.deadLetterMailbox == nil {
		t.Fatal("default deadLetterMailbox must not be nil")
	}
	if opts.deadLetterDeliverTimeout != time.Second {
		t.Fatalf("default deadLetterDeliverTimeout = %v, want 1s", opts.deadLetterDeliverTimeout)
	}
	if opts.handleMsgsTimeout != time.Second {
		t.Fatalf("default handleMsgsTimeout = %v, want 1s", opts.handleMsgsTimeout)
	}
}

func TestRepairConsumerKeepsExplicitValues(t *testing.T) {
	mb := NewDeadLetterLogger()
	opts := &ConsumerOptions{}
	for _, opt := range []ConsumerOption{
		WithReceiveTimeout(3 * time.Second),
		WithMaxRetryLimit(5),
		WithDeadLetterMailbox(mb),
		WithDeadLetterDeliverTimeout(2 * time.Second),
		WithHandleMsgsTimeout(4 * time.Second),
	} {
		opt(opts)
	}
	repairConsumer(opts)

	if opts.receiveTimeout != 3*time.Second {
		t.Fatalf("receiveTimeout = %v, want 3s", opts.receiveTimeout)
	}
	if opts.maxRetryLimit != 5 {
		t.Fatalf("maxRetryLimit = %d, want 5", opts.maxRetryLimit)
	}
	if opts.deadLetterMailbox != mb {
		t.Fatal("explicit deadLetterMailbox was overwritten")
	}
	if opts.deadLetterDeliverTimeout != 2*time.Second {
		t.Fatalf("deadLetterDeliverTimeout = %v, want 2s", opts.deadLetterDeliverTimeout)
	}
	if opts.handleMsgsTimeout != 4*time.Second {
		t.Fatalf("handleMsgsTimeout = %v, want 4s", opts.handleMsgsTimeout)
	}
}

func TestDeadLetterLoggerDeliver(t *testing.T) {
	mb := NewDeadLetterLogger()
	if err := mb.Deliver(nil, &redis.MsgEntity{MsgID: "1-1"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}

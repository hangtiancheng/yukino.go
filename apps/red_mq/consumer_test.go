package red_mq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/red_mq/redis"
)

// newManualConsumer builds a Consumer the same way NewConsumer does but
// without starting the run loop, so tests can wrap run in a goroutine and
// observe its exit.
func newManualConsumer(t *testing.T, client *redis.Client, cb MsgCallback, opts ...ConsumerOption) *Consumer {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	c := &Consumer{
		client:       client,
		ctx:          ctx,
		stop:         stop,
		callbackFunc: cb,
		topic:        "test_topic",
		groupID:      "test_group",
		consumerID:   "test_consumer",
		failureCnts:  make(map[redis.MsgEntity]int),
		opts:         &ConsumerOptions{},
	}
	for _, opt := range opts {
		opt(c.opts)
	}
	repairConsumer(c.opts)
	return c
}

func TestNewConsumerParamErrors(t *testing.T) {
	client := redis.NewClient("tcp", "127.0.0.1:1", "")
	defer client.Close()
	cb := func(ctx context.Context, msg *redis.MsgEntity) error { return nil }

	if c, err := NewConsumer(client, "t", "g", "c", nil); err == nil || c != nil {
		t.Fatalf("nil callback must be rejected, got consumer=%v err=%v", c, err)
	}
	if c, err := NewConsumer(nil, "t", "g", "c", cb); err == nil || c != nil {
		t.Fatalf("nil client must be rejected, got consumer=%v err=%v", c, err)
	}
	if c, err := NewConsumer(client, "", "g", "c", cb); err == nil || c != nil {
		t.Fatalf("empty topic must be rejected, got consumer=%v err=%v", c, err)
	}
}

func TestConsumerHandlerMsgsCountsFailures(t *testing.T) {
	c := &Consumer{
		callbackFunc: func(ctx context.Context, msg *redis.MsgEntity) error {
			return errors.New("boom")
		},
		failureCnts: make(map[redis.MsgEntity]int),
	}
	msg := &redis.MsgEntity{MsgID: "1-1", Key: "k", Val: "v"}

	c.handlerMsgs(context.Background(), []*redis.MsgEntity{msg})

	if got := c.failureCnts[*msg]; got != 1 {
		t.Fatalf("failure count = %d, want 1", got)
	}
}

func TestConsumerRunAcksProcessedMessages(t *testing.T) {
	f := newFakeRedis(t)
	msg := &redis.MsgEntity{MsgID: "1-1", Key: "k", Val: "v"}
	f.setNewMsg(msg, true)

	got := make(chan *redis.MsgEntity, 1)
	c := newManualConsumer(t, f.client(), func(ctx context.Context, m *redis.MsgEntity) error {
		select {
		case got <- m:
		default:
		}
		return nil
	})

	done := make(chan struct{})
	go func() {
		c.run()
		close(done)
	}()
	// Safety net: Stop even when the test fails before reaching its own Stop.
	defer c.Stop()

	select {
	case m := <-got:
		if m.MsgID != msg.MsgID || m.Key != msg.Key || m.Val != msg.Val {
			t.Fatalf("callback got %+v, want %+v", m, msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("callback was never invoked")
	}

	waitForAck(t, f, msg.MsgID)

	c.Stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit after Stop")
	}

	if len(c.failureCnts) != 0 {
		t.Fatalf("failureCnts = %v, want empty after ack", c.failureCnts)
	}
}

func TestConsumerRunDeadLettersAfterMaxRetries(t *testing.T) {
	f := newFakeRedis(t)
	msg := &redis.MsgEntity{MsgID: "2-1", Key: "k", Val: "poison"}
	// Keep handing the message out, like a message stuck in the PEL.
	f.setNewMsg(msg, false)

	mb := newRecordMailbox(nil)
	c := newManualConsumer(t, f.client(),
		func(ctx context.Context, m *redis.MsgEntity) error { return errors.New("boom") },
		func(o *ConsumerOptions) { o.maxRetryLimit = 2 },
		func(o *ConsumerOptions) { o.deadLetterMailbox = mb },
	)

	done := make(chan struct{})
	go func() {
		c.run()
		close(done)
	}()
	// Safety net: Stop even when the test fails before reaching its own Stop.
	defer c.Stop()

	select {
	case <-mb.first:
	case <-time.After(3 * time.Second):
		t.Fatal("dead letter was never delivered")
	}

	// A successfully delivered dead letter must also be acked.
	waitForAck(t, f, msg.MsgID)

	c.Stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit after Stop")
	}

	delivered := mb.delivered()
	if len(delivered) == 0 || delivered[0].MsgID != msg.MsgID {
		t.Fatalf("delivered = %+v, want msg %s", delivered, msg.MsgID)
	}
}

func TestConsumerDeliverFailureSkipsAck(t *testing.T) {
	f := newFakeRedis(t)
	msg := &redis.MsgEntity{MsgID: "3-1", Key: "k", Val: "poison"}
	f.setNewMsg(msg, false)

	mb := newRecordMailbox(errors.New("mailbox down"))
	c := newManualConsumer(t, f.client(),
		func(ctx context.Context, m *redis.MsgEntity) error { return errors.New("boom") },
		func(o *ConsumerOptions) { o.maxRetryLimit = 1 },
		func(o *ConsumerOptions) { o.deadLetterMailbox = mb },
	)

	done := make(chan struct{})
	go func() {
		c.run()
		close(done)
	}()
	// Safety net: Stop even when the test fails before reaching its own Stop.
	defer c.Stop()

	select {
	case <-mb.first:
	case <-time.After(3 * time.Second):
		t.Fatal("dead letter delivery was never attempted")
	}

	c.Stop()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit after Stop")
	}

	// A failed delivery must not ack the message, otherwise it is lost.
	if ids := f.ackedIDs(); len(ids) != 0 {
		t.Fatalf("acked ids = %v, want none after failed dead letter delivery", ids)
	}
	if _, ok := c.failureCnts[*msg]; !ok {
		t.Fatalf("msg %+v missing from failureCnts, it must be retried", msg)
	}
}

func TestConsumerStopConcurrent(t *testing.T) {
	f := newFakeRedis(t) // answers ErrNoMsg, the loop just polls
	c := newManualConsumer(t, f.client(),
		func(ctx context.Context, m *redis.MsgEntity) error { return nil })

	done := make(chan struct{})
	go func() {
		c.run()
		close(done)
	}()
	// Safety net: Stop even when the test fails before reaching its own Stop.
	defer c.Stop()

	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Stop()
		}()
	}
	wg.Wait()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not exit after concurrent Stop calls")
	}
}

func TestNewConsumerRunsAndStops(t *testing.T) {
	f := newFakeRedis(t)
	c, err := NewConsumer(f.client(), "test_topic", "test_group", "test_consumer",
		func(ctx context.Context, m *redis.MsgEntity) error { return nil })
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	// Let the run loop cycle a few times, then stop it.
	time.Sleep(150 * time.Millisecond)
	c.Stop()
	time.Sleep(250 * time.Millisecond)
}

func TestProducerSendMsgConcurrent(t *testing.T) {
	f := newFakeRedis(t)
	p := NewProducer(f.client(), WithMsgQueueLen(100))

	const producers, perProducer = 8, 10
	ids := make(chan string, producers*perProducer)

	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				id, err := p.SendMsg(context.Background(), "test_topic", "k", "v")
				if err != nil {
					t.Errorf("SendMsg: %v", err)
					return
				}
				ids <- id
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicated msg id %s", id)
		}
		seen[id] = true
	}
	if len(seen) != producers*perProducer {
		t.Fatalf("sent %d unique ids, want %d", len(seen), producers*perProducer)
	}
	if got := f.xaddCount(); got != producers*perProducer {
		t.Fatalf("xadd count = %d, want %d", got, producers*perProducer)
	}
}

package red_mq

import "time"

type ProducerOptions struct {
	msgQueueLen int
}

type ProducerOption func(opts *ProducerOptions)

func WithMsgQueueLen(len int) ProducerOption {
	return func(opts *ProducerOptions) {
		opts.msgQueueLen = len
	}
}

func repairProducer(opts *ProducerOptions) {
	if opts.msgQueueLen <= 0 {
		opts.msgQueueLen = 500
	}
}

type ConsumerOptions struct {
	claimIdle time.Duration
	// receiveTimeout is the per-poll receive timeout.
	receiveTimeout time.Duration
	// maxRetryLimit is the max retry count before a message is sent to the dead-letter mailbox.
	maxRetryLimit int
	// deadLetterMailbox is the user-customizable dead-letter sink.
	deadLetterMailbox DeadLetterMailbox
	// deadLetterDeliverTimeout is the timeout for dead-letter delivery.
	deadLetterDeliverTimeout time.Duration
	// handleMsgsTimeout is the timeout for processing a batch of messages.
	handleMsgsTimeout time.Duration
}

type ConsumerOption func(opts *ConsumerOptions)

func WithReceiveTimeout(timeout time.Duration) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.receiveTimeout = timeout
	}
}

func WithMaxRetryLimit(maxRetryLimit int) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.maxRetryLimit = maxRetryLimit
	}
}

func WithDeadLetterMailbox(mailbox DeadLetterMailbox) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.deadLetterMailbox = mailbox
	}
}

func WithDeadLetterDeliverTimeout(timeout time.Duration) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.deadLetterDeliverTimeout = timeout
	}
}

func WithHandleMsgsTimeout(timeout time.Duration) ConsumerOption {
	return func(opts *ConsumerOptions) {
		opts.handleMsgsTimeout = timeout
	}
}

func repairConsumer(opts *ConsumerOptions) {
	// receiveTimeout == 0 maps to XREADGROUP BLOCK 0 (block forever) and a
	// negative value maps to a non-blocking hot poll, so anything <= 0 is
	// replaced with the documented default.
	if opts.receiveTimeout <= 0 {
		opts.receiveTimeout = 2 * time.Second
	}

	if opts.maxRetryLimit <= 0 {
		opts.maxRetryLimit = 3
	}

	if opts.deadLetterMailbox == nil {
		opts.deadLetterMailbox = NewDeadLetterLogger()
	}

	if opts.deadLetterDeliverTimeout <= 0 {
		opts.deadLetterDeliverTimeout = time.Second
	}

	if opts.handleMsgsTimeout <= 0 {
		opts.handleMsgsTimeout = time.Second
	}
}

// WithAbandonedMessageRecovery reclaims messages left by stopped consumers.
// Set idle longer than the handler timeout to avoid stealing active work.
func WithAbandonedMessageRecovery(idle time.Duration) ConsumerOption {
	return func(opts *ConsumerOptions) { opts.claimIdle = idle }
}

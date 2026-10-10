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
	claimIdle                time.Duration
	receiveTimeout           time.Duration
	maxRetryLimit            int
	deadLetterMailbox        DeadLetterMailbox
	deadLetterDeliverTimeout time.Duration
	handleMsgsTimeout        time.Duration
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

func WithAbandonedMessageRecovery(idle time.Duration) ConsumerOption {
	return func(opts *ConsumerOptions) { opts.claimIdle = idle }
}

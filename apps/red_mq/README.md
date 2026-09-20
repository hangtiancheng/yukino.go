<div align="center">

# red_mq

**A message queue on top of Redis Streams — with retries and a dead-letter sink.**

A thin, production-minded Go wrapper over Redis Streams that gives you consumer groups, automatic retry accounting, and a pluggable dead-letter mailbox without pulling in a heavyweight broker.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-apps%2Fred__mq-blue)](go.mod)

</div>

---

## Why Redis Streams?

Redis Streams already provide the hard parts — append-only delivery, consumer groups, per-consumer pending entries lists (PEL), and `XACK`. What is missing is the _policy_: how many times do you retry a failing handler? Where do poison messages go? This package supplies exactly that.

```text
Producer ──XADD──>  topic (stream)
                       │
        XREADGROUP ────┤ consumer group
                       V
                ┌─────────────┐   handler error
                │  Consumer   │ ───────────────┐
                └──────┬──────┘                │
                success│                pending retry
                       V                       │
                    XACK                 failure count > limit
                       │                       V
                       └──────────────>  DeadLetterMailbox
```

## Features

- **Streaming producer** — `SendMsg` wraps `XADD` with an optional approximate max length, so streams stay bounded.
- **Consumer groups** — `NewConsumer` registers the consumer, polls new entries, and also drains its pending-entries list so messages abandoned by a crashed consumer are retried.
- **Per-message retry accounting** — failures are counted per message; once `maxRetryLimit` is exceeded the message is handed to the dead-letter mailbox instead of retried forever.
- **Pluggable dead letters** — implement `DeadLetterMailbox` to push poison messages to another stream, a table, or an alerting system. The default just logs.
- **Graceful stop** — `Stop()` cancels the processing context and drains cleanly.
- **Timeouts everywhere** — per-poll receive timeout, per-batch handling timeout, and per-delivery dead-letter timeout.

## Install

```bash
go get github.com/hangtiancheng/yukino.go/apps/red_mq
```

## Quick start

### Produce

```go
client := redis.NewClient("tcp", "localhost:6379", "")

producer := red_mq.NewProducer(client, red_mq.WithMsgQueueLen(1000))

msgID, err := producer.SendMsg(ctx, "orders", "order-42", `{"total": 19.99}`)
```

### Consume

```go
consumer, err := red_mq.NewConsumer(
	client,
	"orders",        // topic / stream
	"fulfillment",   // consumer group
	"worker-1",      // consumer id
	func(ctx context.Context, msg *redis.MsgEntity) error {
		if err := fulfill(msg.Val); err != nil {
			return err // triggers retry accounting
		}
		return nil
	},
	red_mq.WithMaxRetryLimit(3),
	red_mq.WithReceiveTimeout(2*time.Second),
	red_mq.WithHandleMsgsTimeout(5*time.Second),
	red_mq.WithDeadLetterMailbox(myMailbox),
)
if err != nil {
	log.Fatal(err)
}
defer consumer.Stop()
```

### Dead letters

```go
type AlertMailbox struct{}

func (AlertMailbox) Deliver(ctx context.Context, msg *redis.MsgEntity) error {
	return pageOnCall(ctx, "poison message", msg.MsgID, msg.Val)
}

consumer, _ := red_mq.NewConsumer(client, topic, group, id, handler,
	red_mq.WithDeadLetterMailbox(AlertMailbox{}))
```

## API

### `Producer`

| Method                          | Description                                  |
| ------------------------------- | -------------------------------------------- |
| `NewProducer(client, opts...)`  | Build a producer.                            |
| `SendMsg(ctx, topic, key, val)` | `XADD` and return the new stream message ID. |

| Option               | Default | Description                                 |
| -------------------- | ------- | ------------------------------------------- |
| `WithMsgQueueLen(n)` | `500`   | Approximate stream max length (`MAXLEN ~`). |

### `Consumer`

| Method                                                               | Description                                                                   |
| -------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `NewConsumer(client, topic, groupID, consumerID, callback, opts...)` | Validate parameters, create the consumer group if needed, and start the loop. |
| `Stop()`                                                             | Stop the consumer.                                                            |

| Option                            | Default         | Description                                    |
| --------------------------------- | --------------- | ---------------------------------------------- |
| `WithReceiveTimeout(d)`           | `2s`            | Per-poll blocking read timeout.                |
| `WithMaxRetryLimit(n)`            | `3`             | Failures allowed before dead-lettering.        |
| `WithDeadLetterMailbox(m)`        | logging mailbox | Sink for messages that exceed the retry limit. |
| `WithDeadLetterDeliverTimeout(d)` | `1s`            | Timeout for a single `Deliver` call.           |
| `WithHandleMsgsTimeout(d)`        | `1s`            | Timeout for processing one batch.              |

### `redis` package

The bundled client is a deliberately small surface: `XADD`, `XACK`, `XReadGroup`, `XReadGroupPending`, `XGroupCreate`, plus generic helpers (`Get`, `Set`, `SetNX`, `SetNEX`, `Del`, `Incr`, `Eval`). `MsgEntity` carries `MsgID`, `Key`, and `Val`.

## Testing

The `example/` package runs against a real Redis. Fill in the connection constants and run:

```bash
go test ./example/...
```

## Notes

> [!NOTE]
> Consumers drain the pending-entries list on every loop, so a message whose consumer died is picked up by a live consumer rather than waiting for `XAUTOCLAIM`. Combine with a stable `consumerID` per process and let `WithMaxRetryLimit` bound the retries.

> [!TIP]
> Streams grow forever unless trimmed. `WithMsgQueueLen` applies an approximate `MAXLEN ~` on every `XADD`; raise it for burst tolerance or trim from the consumer side if you need a hard bound.

## License

[MIT](../../LICENSE) © hangtiancheng

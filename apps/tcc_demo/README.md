<div align="center">

# tcc_demo

**Try-Confirm-Cancel, driven by a log you can crash on.**

A distributed transaction manager implementing the TCC (Try-Confirm-Cancel) pattern: every transaction is journaled before the first network call, so a coordinator that dies mid-flight can recover by replaying its own log.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-apps%2Ftcc__demo-blue)](go.mod)

</div>

---

## The pattern

In TCC, each participant exposes three operations:

| Phase       | Intent                                       | Failure semantics                             |
| ----------- | -------------------------------------------- | --------------------------------------------- |
| **Try**     | Reserve resources and freeze them.           | Any Try failure aborts the whole transaction. |
| **Confirm** | Commit the reservation. Must be idempotent.  | Retried until it succeeds.                    |
| **Cancel**  | Release the reservation. Must be idempotent. | Retried until it succeeds.                    |

The hard part is not the happy path — it is what happens when the coordinator crashes between Try and Confirm. This library answers that with a durable **transaction log** and a background **progress monitor**.

```text
Transaction()
     │
     ├─ 1. CreateTX(components...) ─────────────> TXStore (durable)
     ├─ 2. Try all components in parallel ──────> Component.Try
     │        └─ per-component result ──────────> TXStore.TXUpdate
     │
     └─ 3. advanceProgress()
              ├─ all Try succeeded  ─> Confirm all ─> TXStore.TXSubmit(success)
              └─ any Try failed     ─> Cancel all  ─> TXStore.TXSubmit(failure)

background run()
     └─ poll hanging transactions ─> advanceProgress()   (with exponential backoff)
```

## Features

- **Recoverable by design** — the transaction record is created _before_ any participant is contacted, and every Try outcome is persisted. A restarted manager picks up hanging transactions and finishes them.
- **Timeout-inferred failure** — a transaction whose components are still `hanging` after `WithTimeout` is declared failed and cancelled, so reservations never leak indefinitely.
- **Parallel Try phase** — participants are contacted concurrently; the first failure cancels the context and triggers cancellation of the others.
- **Registry-based components** — `Register(component)` binds an ID to an implementation; components are resolved by ID at Confirm/Cancel time.
- **Global lock** — the `TXStore` contract includes `Lock` / `Unlock` (typically a `redis_lock`) so multiple manager instances do not advance the same transaction concurrently.
- **Backoff monitor** — the recovery loop doubles its tick on error, capped at 8× the configured tick.

## Install

```bash
go get github.com/hangtiancheng/yukino.go/apps/tcc_demo
```

## Quick start

### Implement a participant

```go
type PaymentComponent struct{ /* ... */ }

func (p *PaymentComponent) ID() string { return "payment" }

func (p *PaymentComponent) Try(ctx context.Context, req *tcc_demo.TCCReq) (*tcc_demo.TCCResp, error) {
	// Freeze the funds under req.TXID; safe to retry.
	return &tcc_demo.TCCResp{ComponentID: p.ID(), TXID: req.TXID, ACK: true}, nil
}

func (p *PaymentComponent) Confirm(ctx context.Context, txID string) (*tcc_demo.TCCResp, error) {
	// Idempotently commit the frozen funds.
	return &tcc_demo.TCCResp{ComponentID: p.ID(), TXID: txID, ACK: true}, nil
}

func (p *PaymentComponent) Cancel(ctx context.Context, txID string) (*tcc_demo.TCCResp, error) {
	// Idempotently release the frozen funds.
	return &tcc_demo.TCCResp{ComponentID: p.ID(), TXID: txID, ACK: true}, nil
}
```

### Implement a transaction log

`TXStore` persists transaction state and supplies the recovery feed:

```go
type TXStore interface {
	CreateTX(ctx context.Context, components ...TCCComponent) (txID string, err error)
	TXUpdate(ctx context.Context, txID, componentID string, accept bool) error
	TXSubmit(ctx context.Context, txID string, success bool) error
	GetHangingTXs(ctx context.Context) ([]*Transaction, error)
	GetTX(ctx context.Context, txID string) (*Transaction, error)
	Lock(ctx context.Context, expireDuration time.Duration) error
	Unlock(ctx context.Context) error
}
```

### Run a transaction

```go
manager := tcc_demo.NewTXManager(txStore,
	tcc_demo.WithTimeout(30*time.Second),
	tcc_demo.WithMonitorTick(3*time.Second),
)
defer manager.Stop()

_ = manager.Register(paymentComponent)
_ = manager.Register(inventoryComponent)

txID, ok, err := manager.Transaction(ctx,
	&tcc_demo.RequestEntity{ComponentID: "payment",   Request: map[string]any{"amount": 42}},
	&tcc_demo.RequestEntity{ComponentID: "inventory", Request: map[string]any{"sku": "A-1"}},
)
log.Printf("tx=%s committed=%v err=%v", txID, ok, err)
```

## API

### `TXManager`

| Method                           | Description                                                                   |
| -------------------------------- | ----------------------------------------------------------------------------- |
| `NewTXManager(txStore, opts...)` | Build the manager and start the recovery loop.                                |
| `Register(component)`            | Register a TCC participant by ID.                                             |
| `Transaction(ctx, reqs...)`      | Journal, Try all, then Confirm or Cancel. Returns `(txID, committed, error)`. |
| `Stop()`                         | Stop the recovery loop.                                                       |

### Options

| Option               | Default | Description                                                            |
| -------------------- | ------- | ---------------------------------------------------------------------- |
| `WithTimeout(d)`     | `5s`    | Overall transaction timeout and the hanging-transaction expiry window. |
| `WithMonitorTick(d)` | `10s`   | Base interval of the recovery loop; doubles on error up to 8×.         |

### State model

`Transaction.Status` is derived from per-component Try results:

- `successful` — every component's Try succeeded.
- `failure` — any Try failed, **or** some component is still hanging past the timeout.
- `hanging` — some component is unresolved and the transaction has not timed out yet. Skipped by the monitor until it does.

## Testing

```bash
make cover
# or
go test -v -coverprofile=codecov.report -covermode=atomic ./...
```

The suite uses `go-sqlmock`, `go.uber.org/mock`-generated fakes, and a mock `TXStore`, so it runs without Redis or MySQL.

> [!IMPORTANT]
> `Confirm` and `Cancel` **must be idempotent**. The manager retries them on every recovery pass until they acknowledge. Design participants so a second `Confirm(txID)` is a no-op that returns success.

## License

[MIT](../../LICENSE) © hangtiancheng

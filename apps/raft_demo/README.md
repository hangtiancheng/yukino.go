<div align="center">

# raft_demo

**Raft you can read in one sitting — plus a key-value store on top.**

A from-scratch Go implementation of the Raft consensus algorithm (leader election, log replication, cluster membership changes, `ReadIndex`) exposed through a small HTTP key-value API.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-apps%2Fraft__demo-blue)](go.mod)

</div>

---

> [!WARNING]
> This is a **demo / teaching implementation**. The transport is stubbed out (messages are produced but not actually shipped between processes) and the default storage is in-memory. Do not use it for real data.

## Architecture

```text
      HTTP API  (PUT /<key>, POST /<nodeID>)
           │                    │
     proposeC              confChangeC
           │                    │
           └────────┬───────────┘
                    V
             ┌─────────────┐        Ready()         ┌──────────────┐
             │ raftProxy   │ <───────────────────── │   raft.Node  │
             │  (driver)   │                        │  (core FSM)  │
             └──────┬──────┘                        └──────┬───────┘
                    │ commitC                              │
                    V                                      V
             ┌─────────────┐                        ┌──────────────┐
             │  kvStore    │                        │ raft.Storage │
             │ state mach. │                        │ (in-memory)  │
             └─────────────┘                        └──────────────┘
```

- **`raft/`** — the consensus core. Pure state machine, no networking: `raft`, `follower.go`, `candidate.go`, `leader.go`, `log.go`, `progress.go`, `read.go`, `storage.go`, `node.go`, `ready.go`.
- **`proxy.go`** — the driver. Ticks the node, drains `Ready()`, persists state, applies committed entries, and calls `Advance()`.
- **`kv_store.go`** — the application state machine; consumes `commitC`.
- **`http_api.go`** — client-facing HTTP surface.

## Features

- **Full election state machine** — Follower, PreCandidate, Candidate, Leader, with randomized election timeouts and pre-vote to avoid disruptive servers.
- **Log replication** — leader append, follower append/heartbeat handling, match/next index tracking per peer, and commit-index advancement by quorum.
- **Membership changes** — `ConfChangeAddNode` / `ConfChangeRemoveNode` applied through the log.
- **ReadIndex** — linearizable reads without appending to the log.
- **Storage abstraction** — `Storage` interface with a ready-to-use `MemoryStorage`.
- **Batching by design** — the `Ready()` / `Advance()` cycle groups hard state, entries, messages, and committed entries into one atomic-ish unit of work.

## Run

```bash
go run .
# HTTP API on :8091
```

## HTTP API

| Request                                 | Effect                                          |
| --------------------------------------- | ----------------------------------------------- |
| `PUT /<key>` — body is the value        | Propose a write through Raft.                   |
| `POST /<nodeID>` — body is node context | Propose a `ConfChangeAddNode` for that node ID. |

```bash
# propose a write
curl -X PUT http://localhost:8091/name -d 'yukino'

# add node 2 to the cluster
curl -X POST http://localhost:8091/2 -d 'node-2-context'
```

Committed entries are delivered on `commitC` and applied by `kvStore`.

## Embedding it

The core is a `Node` you can drive from any transport:

```go
c := raft.Config{
	ID:            1,
	ElectionTick:  10,
	HeartbeatTick: 1,
	Storage:       raft.NewMemoryStorage(),
}

node := raft.StartNode(&c, []raft.Peer{{ID: 1}})

go func() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		node.Tick()
	}
}()

for {
	select {
	case <-node.Ready():
		// 1. persist HardState / ConfState
		// 2. persist unstable entries
		// 3. send messages to peers
		// 4. apply committed entries to your state machine
		node.Advance()
	}
}

_ = node.Propose(ctx, []byte("key=value"))
_ = node.ProposeConfChange(ctx, raft.ConfChange{NodeID: 2, Type: raft.ConfChangeAddNode})
_ = node.ReadIndex(ctx, nil)
```

## Core types

| Type         | Purpose                                                                                                                          |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| `Node`       | Public driver interface: `Tick`, `Campaign`, `Propose`, `ProposeConfChange`, `ReadIndex`, `ApplyConfChange`, `Ready`, `Advance`. |
| `Config`     | Node ID, tick counts, storage, and election knobs.                                                                               |
| `Entry`      | A log entry (`Index`, `Term`, `Type`, `Data`).                                                                                   |
| `Message`    | The wire-level Raft RPC union (append, vote, heartbeat, snapshot, read index, …).                                                |
| `HardState`  | Durable `Term` / `Vote` / `Commit`.                                                                                              |
| `SoftState`  | Volatile `Lead` / `RaftState`.                                                                                                   |
| `ConfChange` | Membership operation applied through the log.                                                                                    |
| `Storage`    | Interface for term, entries, last index, and hard-state persistence.                                                             |

## Testing

```bash
go test ./raft/...
```

## License

[MIT](../../LICENSE) © hangtiancheng

<div align="center">

# lsm_tree

**A log-structured merge tree, from WAL to leveled compaction.**

A complete, dependency-free LSM storage engine written in Go: write-ahead log, lock-free-friendly skiplist memtable, block-based SSTables with bloom filters, and background leveled compaction.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-apps%2Flsm__tree-blue)](go.mod)

</div>

---

## What's inside

```text
                 Put(key, value)
                       │
                 ┌─────V─────┐      full
                 │  memtable │ ──────────> immutable memtables ──> L0 SSTable
                 │ (skiplist)│             (flush queue)
                 └─────┬─────┘
         read          │
   Get(key) ───────────┤
                       │
      ┌────────────────V────────────────────────────────┐
      │  L0    [sst] [sst] [sst]        newest first    │
      │  L1    [──── sst ────] [── sst ──]              │  background
      │  L2    [────────── sst ──────────]              │  leveled
      │  ...                                            │  compaction
      └─────────────────────────────────────────────────┘
```

| Component    | Package           | Notes                                                                                                                                            |
| ------------ | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Durability   | `wal`             | Every `Put` is appended to the WAL before the memtable, so a crash never loses acknowledged writes. Restore replays WAL files into the memtable. |
| Write buffer | `memtable`        | Skiplist; sorted iteration feeds SSTable construction.                                                                                           |
| Prediction   | `filter`          | Per-block bloom filters cut disk reads for absent keys.                                                                                          |
| Persistence  | `sst_*`           | Data / filter / index blocks plus a 32-byte footer. Keys within an SSTable are sorted.                                                           |
| Read path    | `node.go`         | Binary-searches the block index, consults the bloom filter, then reads the block.                                                                |
| Reclamation  | `tree_compact.go` | Flushes immutable memtables to L0; merges levels when they exceed their size budget.                                                             |
| Helpers      | `util`            | Shared-prefix length and separator-key computation for block boundaries.                                                                         |

## Features

- **Write-ahead logging** — `wal.WALWriter` / `wal.WALReader` with CRC-checked records and `RestoreToMemtable`.
- **Block-structured SSTables** — a footer locates the filter and index blocks; the index stores a separator key (≥ previous block's max, < next block's min) so lookups are a binary search, not a scan.
- **Bloom filters** — sized per data block, dramatically reducing pointless reads for missing keys.
- **Multi-level topology** — `SSTNumPerLevel` sstables per level, and each deeper level multiplies the size limit by 10.
- **Concurrent reads, guarded writes** — a global data lock plus per-level locks keep `Get` from blocking on background compaction more than necessary.
- **Plug-and-play** — inject a custom `filter.Filter` or a `memtable.MemTableConstructor` via config.

## Install

```bash
go get github.com/hangtiancheng/yukino.go/apps/lsm_tree
```

## Quick start

```go
package main

import (
	"log"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree"
)

func main() {
	conf, err := lsm_tree.NewConfig("./data",
		lsm_tree.WithMaxLevel(7),
		lsm_tree.WithSSTSize(4<<20),        // L0 sstable size, 4 MiB
		lsm_tree.WithSSTNumPerLevel(10),
		lsm_tree.WithSSTDataBlockSize(16<<10),
	)
	if err != nil {
		log.Fatal(err)
	}

	tree, err := lsm_tree.NewTree(conf)
	if err != nil {
		log.Fatal(err)
	}
	defer tree.Close()

	if err := tree.Put([]byte("hello"), []byte("world")); err != nil {
		log.Fatal(err)
	}

	value, found, err := tree.Get([]byte("hello"))
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("found=%v value=%s", found, value)
}
```

`NewConfig` creates the sstable directory and a `walfile/` subdirectory if they don't exist, and `NewTree` reconstructs the topology from existing SSTables before replaying the WAL.

## Configuration

| Option                        | Default      | Description                                                          |
| ----------------------------- | ------------ | -------------------------------------------------------------------- |
| `WithMaxLevel(n)`             | `7`          | Number of levels in the tree.                                        |
| `WithSSTSize(bytes)`          | `1 MiB`      | Size budget of a level-0 SSTable; multiplied by `10^n` at depth `n`. |
| `WithSSTNumPerLevel(n)`       | `10`         | Expected number of SSTables per level before compaction.             |
| `WithSSTDataBlockSize(bytes)` | `16 KiB`     | Data block size inside an SSTable.                                   |
| `WithFilter(f)`               | bloom filter | Custom `filter.Filter`.                                              |
| `WithMemtableConstructor(c)`  | skiplist     | Custom memtable implementation.                                      |

## On-disk layout

```
data/
├── 0_1.sst, 0_2.sst, ...     # level_seq.sst — sstables, level-prefixed
└── walfile/
    └── <memtable-index>.wal  # one WAL per memtable generation
```

Each SSTable is `[data blocks][filter blocks][index block][footer]`. The footer is four varints (32 bytes total) pointing at the filter and index blocks, which is why `SSTFooterSize` is fixed.

## Testing

```bash
go test ./...
```

The suite covers block encoding, the skiplist, bloom-filter false-positive bounds, WAL write/restore, and end-to-end put/get/compact behavior.

> [!WARNING]
> This is an educational/reference implementation. It has no MVCC snapshot isolation, no range tombstones, and no fsync policy tuning. Do not store data you cannot lose.

## License

[MIT](../../LICENSE) © hangtiancheng

---
name: yukino-orm
description: >
  Build, modify, or debug github.com/hangtiancheng/yukino.go/libs/yukino_orm,
  the Go MongoDB query builder. Use for Engine and mutable Query APIs, filters,
  CRUD, scalar and grouped aggregation, cursors, transactions, and sequences.
---

# yukino_orm

Source root and import:
`libs/yukino_orm/`,
`github.com/hangtiancheng/yukino.go/libs/yukino_orm`.
It uses `go.mongodb.org/mongo-driver/v2`.
Read the relevant implementation for signatures and semantics; dependencies
and toolchain requirements come from `go.mod` and the repository's `go.work`.

## Source guide

| Work                                                               | Read                                                                                       |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| Connections, collection/model entrypoints, transactions, sequences | [engine.go](../../../libs/yukino_orm/engine.go)                                            |
| Mutable builder state and cloning                                  | [query.go](../../../libs/yukino_orm/query.go)                                              |
| Conditions, operator parsing, filter/projection generation         | [filter.go](../../../libs/yukino_orm/filter.go)                                            |
| CRUD, preflight errors, update normalization, insert expansion     | [query_exec.go](../../../libs/yukino_orm/query_exec.go)                                    |
| Scalar aggregation, distinct values, plucking                      | [query_aggregate.go](../../../libs/yukino_orm/query_aggregate.go)                          |
| Grouped aggregation and result-column validation                   | [query_group.go](../../../libs/yukino_orm/query_group.go)                                  |
| Cursor ownership and iteration                                     | [query_stream.go](../../../libs/yukino_orm/query_stream.go)                                |
| Collection-name derivation and exported log helpers                | [naming.go](../../../libs/yukino_orm/naming.go), [log.go](../../../libs/yukino_orm/log.go) |

## Engine and query ownership

- `NewEngine(ctx, uri, database) (*Engine, error)` rejects blank URI/database,
  creates a driver client, and pings with the supplied context.
  The constructor exposes URI configuration, not a client-options parameter.
  Reuse the engine and close it with `Close(ctx)`.
- `Collection(name)` and `Model(value)` return mutable `*Query` builders.
  An absent engine/database or blank collection name leaves an unbound
  query; execution then reports `ErrCollectionRequired`.
- `Model` derives the collection from the Go type name: unwrap pointers
  and one slice/array level, require a named struct, insert underscores
  before uppercase ASCII letters, then apply simple pluralization.
  Acronyms are split letter by letter; irregular plurals are not handled.
  Use `Collection("explicit_name")` for an explicit destination.
  Query field names refer to BSON keys; collection naming does not rename
  document fields. Declare BSON tags for the schema used by queries.
- Chain methods mutate and return the receiver. Use `Clone()` at branch
  points; build a separate query per goroutine. Clone copies builder slices,
  including projection and group state, but shares condition values such
  as caller-supplied maps/slices, and shares engine/collection handles.
  `Select(fields...)` initially retains the supplied slice; a subsequent
  `Clone` copies that projection slice.
- Parsing errors are stored as the builder's first error and returned by
  execution. Direct helpers such as `WhereIn` and `WhereNot` append
  conditions without the general parser's field validation. Preflight
  returns a recorded builder error before checking for an unbound collection.
- Engine methods pass caller contexts through to the driver without adding
  operation timeouts. Use contexts appropriate to the operation and a
  live shutdown context for `Close`.

## Filters and projection

`Where` accepts `(bson.M)`/`(map[string]any)`, `(field, value)`, or
`(field, operator, value)`. Map keys are processed in sorted order.
Nil equality values become null conditions.
Repeated conditions on a field preserve conjunctions: distinct operators
may share an operator document; conflicting conditions use `$and`.

- Operators include `=`/`==`, `!=`/`<>`, comparisons, `in`,
  `not in`/`nin`, `like`, `ilike`, `between`, and `not between`.
  Raw `$`-prefixed operators pass through without shape validation.
- Between is inclusive and takes a two-element slice/array in the general
  `Where` form. `WhereBetween(field, low, high)` and
  `WhereNotBetween(field, low, high)` provide explicit helpers.
- LIKE patterns are regex-escaped, replace `%` with `.*` and `_`
  with `.`, and are anchored at both ends. `WhereILike` adds the
  case-insensitive option. No wildcard escape syntax is implemented.
- All ordinary `Where*` conditions form one AND branch. Each
  `OrWhere*` adds a separate OR branch; a map-form `OrWhere` keeps
  its map conditions together. Thus
  `Where(a).OrWhere(b).Where(c)` means `(a AND c) OR b`.
  An empty map-form `OrWhere` is a no-op.
- Available OR helpers are `OrWhereNot`, `OrWhereIn`, `OrWhereNotIn`,
  `OrWhereNull`, `OrWhereNotNull`, and `OrWhereBetween`.
  Use general `OrWhere(field, op, value)` for other supported operators.
- `OrderBy(field, direction...)` appends sort fields; trimmed,
  case-insensitive `desc` selects descending, otherwise ascending.
  `Limit` and `Offset` apply only for positive values.
- `Select(fields...)` replaces the projection. A `-` prefix excludes a
  field; other names include it. Projection validity is left to MongoDB.

## Execution

| API                                          | Behavior that affects callers                                                                                                                                                                              |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Insert(ctx, documents...)`                  | Returns `InsertResult{InsertedIDs, InsertedCount}`. A single slice/array argument expands into documents except `bson.D` and byte-element containers. Partial multi-insert results can accompany an error. |
| `First(ctx, out)`                            | Applies filter, sort, offset, projection; ignores Limit. No match returns `ErrNotFound`, which aliases `mongo.ErrNoDocuments`.                                                                             |
| `Find(ctx, out)`                             | Applies find options and decodes all documents, closing the internal cursor.                                                                                                                               |
| `Update(ctx, update)`                        | Calls UpdateMany and returns matched count. Plain maps, `bson.D`, and structs become `$set`; documents containing an operator key pass through.                                                            |
| `Upsert(ctx, update)`                        | Calls UpdateMany with upsert and returns matched/modified/upserted counts plus the upserted ID.                                                                                                            |
| `Increment/Decrement(ctx, field, amount...)` | Uses `$inc`, defaults to one, and returns matched count.                                                                                                                                                   |
| `Delete(ctx)`                                | Calls DeleteMany and returns deleted count.                                                                                                                                                                |
| `Count(ctx)`, `Exists(ctx)`                  | Count the full filter; sort, limit, and offset do not restrict the count.                                                                                                                                  |
| `EnsureIndexes(ctx, []mongo.IndexModel)`     | Creates indexes; an empty list returns nil without a driver call after preflight.                                                                                                                          |
| `DropCollection(ctx)`                        | Drops the bound collection.                                                                                                                                                                                |

Update and delete have no empty-filter guard. Sort/limit/offset do not turn
these multi-document writes into single-document writes.
For partial updates, choose BSON fields explicitly; struct updates follow
the struct's BSON encoding and tags.

`Distinct(ctx, field)` returns `[]any`; `CountDistinct` materializes those
values and counts them locally. `Sum/Avg/Min/Max(ctx, field)` decode the
result into `float64` and return zero when the pipeline returns no rows.
These scalar operations ignore sort, limit, offset, and projection.

`Pluck(ctx, field, out)` requires a non-nil pointer to a slice, applies sort,
limit, and offset, and builds its own projection. Dotted fields are looked
up as BSON paths; missing fields contribute the element type's zero value.

## Grouping, cursors, transactions

- `GroupBy(fields...)`, `Having(args...)`, and
  `CountAs(alias)/SumAs(field, alias)/AvgAs/MinAs/MaxAs` configure
  `Aggregate(ctx, out)`. Aggregate requires GroupBy. Group state is rejected
  by every other terminal operation.
- The pipeline is optional document `$match`, `$group`, `$project`,
  optional having `$match`, then sort, skip, and limit.
  Dotted group fields become result columns with dots replaced by underscores.
  Having and OrderBy must reference those result columns or aggregate aliases.
- `Select` cannot be combined with grouped Aggregate. Aliases cannot be
  blank, `_id`, start with `$`, contain dots, repeat, or collide with a
  group result column. Flattened group-column collisions also fail.
- `Cursor(ctx)` exposes `Next/Decode/Current/Err/Close`; its caller owns
  closure. `Each(ctx, fn)` iterates and closes for the caller, stopping on
  callback error. Query builders and cursors are not safe for concurrent
  mutation/use.
- `Transaction(ctx, func(sc context.Context, tx *Engine) error)` delegates
  to the driver's `WithTransaction`, then ends the session. Use the callback's
  context and engine for its operations; do not retain session-bound objects
  after the callback or use the session concurrently.
- A transaction sub-engine binds its session when an operation's context
  does not already contain one. It does not replace an existing context
  session. The same binding is used by cursor Next/Close and NextSequence.
- `NextSequence(ctx, name)` atomically increments `value` in the hard-coded
  `counters` collection with `_id: name`, upserts, and returns the new
  `int64` value.

Exported `Info/Infof/Error/Errorf` and `SetLevel` configure application
logging helpers. The ORM execution code does not call them for query logging.

## Example and verification

```go
package ormexample

import (
    "context"

    orm "github.com/hangtiancheng/yukino.go/libs/yukino_orm"
)

type CityTotal struct {
    City  string  `bson:"city"`
    N     int64   `bson:"n"`
    Total float64 `bson:"total"`
}

func PaidTotals(ctx context.Context, engine *orm.Engine) ([]CityTotal, error) {
    var rows []CityTotal
    err := engine.Collection("orders").
        Where("status", "paid").
        GroupBy("city").CountAs("n").SumAs("amount", "total").
        Having("n", ">=", 2).OrderBy("total", "desc").Limit(10).
        Aggregate(ctx, &rows)
    return rows, err
}
```

From `libs/yukino_orm`, pure builder checks can run without MongoDB:

```sh
go test -run 'TestBuild|TestWhere(Object|Operator|Invalid|NotBetween)|TestLike|TestPluralize|TestNormalize|TestExpand|TestClone|TestGroup(Pipeline|Builder|Result|Flattened)|TestOrderBy|TestCollectionName|TestNewEngine|TestSingleDotted|TestAggAlias|TestOrWhereEmptyMap' .
```

The full `go test ./...` suite includes integration tests using
`openTestEngine` in `yukino_orm_test.go`. It reads `MONGO_URI`, defaulting
to `mongodb://localhost:27017/`, and creates/drops a unique test database.
Connection failure fails those tests; an authorization failure in the
collection-access probe skips them. Transaction integration also needs a
MongoDB deployment supporting transactions; the test skips recognized
standalone-server rejections.

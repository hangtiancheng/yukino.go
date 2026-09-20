# Yukino ORM

A lightweight MongoDB ORM for Go, designed around a chainable query builder inspired by Knex.js. It provides a fluent, expressive API for developers accustomed to method chaining while retaining the full power of the official MongoDB driver underneath.

Built on top of `go.mongodb.org/mongo-driver/v2`, the library exposes two core abstractions: `Engine` (connection and session management) and `Query` (chainable query builder). The entire module is flat (no sub-packages), easy to read, and suitable as a direct dependency in application code or as a foundation for higher-level wrappers.

Module path: `github.com/hangtiancheng/yukino.go/yukino_orm`

## Features

- Chainable query builder with Where (equality / operator / object form) / WhereNot / OrWhere and Or-variants / WhereIn / WhereNotIn / WhereBetween / WhereNotBetween / WhereNull / WhereNotNull / WhereLike / WhereILike predicates
- Safe filter construction: conditions on the same field are always AND-combined (never silently overwritten); invalid builder input surfaces as an error at execution instead of panicking
- Automatic `$set` wrapping: plain `bson.M`, `map[string]any`, `bson.D`, and struct updates without MongoDB operators are transparently wrapped in `$set`
- knex-style write helpers: Upsert, Increment / Decrement, Insert accepts a slice of documents
- Built-in aggregation methods: Count / CountDistinct / Sum / Avg / Min / Max / Distinct / Pluck
- Query Clone for deriving variants from a shared base without state sharing
- Reflection-based collection naming: pass a struct and the collection name is derived automatically (snake_case + pluralization)
- Transaction support: wraps MongoDB sessions with automatic commit and rollback; queries made through the transaction sub-Engine automatically join the session
- Auto-increment sequences: classic `counters` collection pattern for globally unique sequential IDs
- Index management: create multiple indexes in a single call via EnsureIndexes
- Colored, level-controlled logger with independent toggle

## Installation

```bash
go get github.com/hangtiancheng/yukino.go/yukino_orm
```

Requires Go 1.26 or later. A running MongoDB instance is required for integration tests.

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/hangtiancheng/yukino.go/yukino_orm"
    "go.mongodb.org/mongo-driver/v2/bson"
)

type User struct {
    ID        int64     `bson:"_id"`
    Name      string    `bson:"name"`
    Email     string    `bson:"email"`
    Age       int       `bson:"age"`
    CreatedAt time.Time `bson:"created_at"`
}

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    engine, err := yukino_orm.NewEngine(ctx, "mongodb://localhost:27017", "demo")
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close(ctx)

    // Insert documents
    _, err = engine.Model(&User{}).Insert(ctx,
        &User{ID: 1, Name: "Alice", Email: "alice@example.com", Age: 28, CreatedAt: time.Now()},
        &User{ID: 2, Name: "Bob",   Email: "bob@example.com",   Age: 19, CreatedAt: time.Now()},
    )
    if err != nil {
        log.Fatal(err)
    }

    // Chainable query
    var adults []User
    err = engine.Model(&User{}).
        Where("age", ">=", 18).
        WhereNotNull("email").
        OrderBy("created_at", "desc").
        Limit(10).
        Find(ctx, &adults)
    if err != nil {
        log.Fatal(err)
    }

    // Update (plain bson.M without $ operators is automatically wrapped in $set)
    modified, _ := engine.Model(&User{}).
        Where("name", "Alice").
        Update(ctx, bson.M{"age": 29})
    log.Printf("modified=%d", modified)
}
```

## Core Concepts

### Engine

`Engine` manages the MongoDB connection and database handle. It serves as the entry point for all queries. Nil-receiver safety is built in: calling getters on a nil Engine returns zero values without panicking.

```go
engine, err := yukino_orm.NewEngine(ctx, uri, dbName)

engine.Client()        // *mongo.Client
engine.Database()      // *mongo.Database
engine.DatabaseName()  // string

engine.Close(ctx)        // disconnect
engine.DropDatabase(ctx) // drop the entire database
```

During construction, `NewEngine` issues a Ping. If the ping fails, the connection is immediately closed and an error is returned, preventing dangling handles.

### Query

A `Query` is created via `engine.Collection(name)` or `engine.Model(value)`. It accumulates conditions, sort order, pagination, and projection state. All chainable methods return the same `Query` pointer. Actual communication with MongoDB only occurs when a terminal method (Find, Insert, Update, etc.) is invoked.

```go
q := engine.Collection("users")   // explicit collection name
q := engine.Model(&User{})        // derived via reflection -> "users"
q := engine.Model([]*User{})      // slices also work -> "users"
```

### Collection Naming Convention

`engine.Model(value)` derives the collection name through reflection:

1. Strip pointers and slices to reach the underlying struct type
2. Convert CamelCase to snake_case
3. Apply English pluralization rules

| Struct        | Collection Name  |
| ------------- | ---------------- |
| `User`        | `users`          |
| `ChatHistory` | `chat_histories` |
| `Address`     | `addresses`      |
| `Category`    | `categories`     |
| `Day`         | `days`           |

Pluralization rules cover common cases: consonant + y becomes -ies; endings in s / x / sh / ch receive -es; everything else gets -s. For irregular plurals (e.g., person/people) or acronyms (e.g., HTTPServer), use `engine.Collection("custom_name")` directly.

## Query Builder

### Condition Methods

```go
q.Where("name", "Alice")                  // name == "Alice"
q.Where("deleted_at", nil)                // nil value becomes a null check
q.Where("age", ">=", 18)                  // age >= 18
q.Where(bson.M{"a": 1, "b": 2})           // object form: a == 1 AND b == 2
q.WhereNot("role", "guest")               // role != "guest"
q.WhereIn("status", []string{"a", "b"})   // status in ["a", "b"]
q.WhereNotIn("role", []string{"guest"})   // role not in ["guest"]
q.WhereNull("deleted_at")                 // deleted_at == null
q.WhereNotNull("email")                   // email != null
q.WhereBetween("age", 18, 30)             // 18 <= age <= 30
q.WhereNotBetween("age", 18, 30)          // NOT (18 <= age <= 30)
q.WhereLike("name", "To%")                // SQL LIKE pattern, case-sensitive
q.WhereILike("name", "to_")               // SQL LIKE pattern, case-insensitive
q.OrWhere("vip", true)                    // combined with the main chain via $or
q.OrWhere(bson.M{"a": 1, "b": 2})         // OR (a == 1 AND b == 2)
q.OrWhereIn("age", []int{25, 30})         // OR age in [25, 30]
// Also available: OrWhereNot / OrWhereNotIn / OrWhereNull / OrWhereNotNull / OrWhereBetween
```

Supported comparison operators (second argument in `Where(field, op, value)`,
case-insensitive):

```
=  ==  !=  <>  >  >=  <  <=  in  not in  like  ilike  between  not between
```

Operators starting with `$` (e.g. `"$regex"`, `"$exists"`, `"$in"`) are passed
through to MongoDB directly. Any other unrecognized operator is rejected: the
builder records the error and the next execution method returns it instead of
sending a broken query (or panicking). The same applies to malformed arguments
such as a non-string field name or a wrong argument count.

`WhereLike` / `WhereILike` translate SQL LIKE patterns into anchored regular
expressions: `%` matches any sequence, `_` matches a single character, and all
regex metacharacters in the pattern are escaped.

Multiple conditions on the same field are always AND-combined; nothing is
silently overwritten:

```go
q.Where("age", ">", 18).Where("age", "<", 30)
// { age: { $gt: 18, $lt: 30 } }

q.Where("age", 18).Where("age", ">", 10)
// { age: 18, $and: [ { age: { $gt: 10 } } ] }

q.OrWhere("a", 1).OrWhere("b", 2)   // no preceding Where
// { $or: [ { a: 1 }, { b: 2 } ] }  -- no match-all empty branch
```

Note: a `Where` added after an `OrWhere` still joins the main AND chain, i.e.
`Where(a).OrWhere(b).Where(c)` produces `(a AND c) OR b`. This differs from
SQL operator precedence; keep OrWhere calls last for clarity.

### Sorting, Pagination, and Projection

```go
q.OrderBy("created_at")           // ascending (default)
q.OrderBy("age", "desc")          // descending; direction is case-insensitive
q.Limit(20)
q.Offset(40)
q.Select("name", "email")         // inclusive projection
q.Select("name", "-_id")          // "-" prefix excludes a field
```

MongoDB only allows mixing inclusion with the exclusion of `_id`; other
mixtures are rejected by the server.

### Cloning

`Query` is a mutable struct. To branch multiple variants from a shared base,
use `Clone`, which deep-copies all builder state:

```go
base := engine.Collection("users").Where("active", true)
adults := base.Clone().Where("age", ">=", 18)
minors := base.Clone().Where("age", "<", 18)
```

### Terminal Methods

```go
// Insert: single, multiple, or a slice of documents
res, err := q.Insert(ctx, doc1, doc2, doc3)
res, err := q.Insert(ctx, []*User{u1, u2})   // slices are expanded automatically
// res.InsertedIDs / res.InsertedCount

// Retrieve a single document
var u User
err := q.First(ctx, &u)            // returns ErrNotFound (mongo.ErrNoDocuments) if nothing matches

// Retrieve multiple documents
var us []User
err := q.Find(ctx, &us)

// Update: returns the number of MATCHED documents (knex-style affected rows).
// Plain documents without $ operators (bson.M, map, bson.D, struct) are
// auto-wrapped in $set.
matched, err := q.Update(ctx, bson.M{"age": 29})
matched, err := q.Update(ctx, bson.M{"$inc": bson.M{"login_count": 1}})

// Upsert: update matching documents, insert when nothing matches
res, err := q.Where("_id", 1).Upsert(ctx, bson.M{"name": "Alice"})
// res.MatchedCount / res.ModifiedCount / res.UpsertedCount / res.UpsertedID

// Atomic counters (integer amounts; default 1)
matched, err := q.Increment(ctx, "login_count")
matched, err := q.Decrement(ctx, "credits", 5)

// Delete
deleted, err := q.Delete(ctx)

// Count and existence check
n, err := q.Count(ctx)
ok, err := q.Exists(ctx)

// Index management
names, err := q.EnsureIndexes(ctx, []mongo.IndexModel{...})

// Drop an entire collection
err := q.DropCollection(ctx)
```

### Aggregation Methods

Aggregation methods automatically respect the current condition chain. Internally they execute a MongoDB aggregation pipeline consisting of `[$match, $group]`:

```go
sum, err := engine.Collection("orders").Where("status", "paid").Sum(ctx, "amount")
avg, err := engine.Collection("orders").Avg(ctx, "amount")
mn,  err := engine.Collection("orders").Min(ctx, "amount")
mx,  err := engine.Collection("orders").Max(ctx, "amount")

vals, err := engine.Collection("users").Distinct(ctx, "city")       // []any
n,    err := engine.Collection("users").CountDistinct(ctx, "city")  // number of distinct values

// Pluck collects one field into a slice of the value type. Sort/limit/offset
// are honored and the Query's projection is left untouched.
var names []string
err = engine.Collection("users").Where("active", true).OrderBy("name").Pluck(ctx, "name", &names)
```

Sum / Avg / Min / Max return `float64` and are designed for numeric fields.
Sum / Avg return 0 for missing or non-numeric fields; Min / Max on non-numeric
fields (e.g. strings, dates) fail at decode -- use Distinct or a custom
aggregation pipeline for those.

### Grouped Aggregation (GroupBy / Having)

`GroupBy` + accumulator aliases + `Aggregate` run a `$match -> $group ->
$project -> $match(having) -> $sort -> $skip -> $limit` pipeline. Each result
row contains the group keys and aliases as top-level fields:

```go
type cityAgg struct {
    City  string  `bson:"city"`
    N     int64   `bson:"n"`
    Total float64 `bson:"total"`
}

var rows []cityAgg
err := engine.Collection("orders").
    Where("status", "paid").          // filter before grouping
    GroupBy("city").                  // one or more group keys
    CountAs("n").                     // per-group document count
    SumAs("amount", "total").         // also: AvgAs / MinAs / MaxAs
    Having("n", ">=", 2).             // filter after grouping (same forms as Where)
    OrderBy("total", "desc").         // references result columns
    Limit(10).
    Aggregate(ctx, &rows)
```

Rules enforced at build time (returned as errors, never silently wrong):

- `Aggregate` requires at least one `GroupBy` key.
- Dotted group keys are flattened in the result (`addr.city` -> `addr_city`);
  duplicate flattened keys are rejected.
- `Having` and `OrderBy` may only reference result columns (flattened group
  keys or aliases).
- Aliases must not collide with group keys, repeat, be `_id`, start with `$`,
  or contain `.`.
- `Select` cannot be combined with `GroupBy`.
- Pending GroupBy/Having state on any other execution method (`Find`, `Count`,
  `Delete`, ...) is an error instead of being silently ignored.

### Streaming (Cursor / Each)

For large result sets, stream documents one at a time instead of loading
everything with `Find`:

```go
// Callback style: the cursor is closed automatically.
err := engine.Collection("orders").
    Where("status", "paid").
    OrderBy("amount", "desc").
    Each(ctx, func(c *yukino_orm.Cursor) error {
        var o Order
        if err := c.Decode(&o); err != nil {
            return err
        }
        return process(o) // returning an error stops iteration
    })

// Manual style: remember to Close.
cursor, err := engine.Collection("orders").Cursor(ctx)
if err != nil { ... }
defer cursor.Close(ctx)
for cursor.Next(ctx) {
    var o Order
    if err := cursor.Decode(&o); err != nil { ... }
}
if err := cursor.Err(); err != nil { ... }
```

Both honor the Query's filter, sort, limit, offset, and projection, and join
the transaction session when used through a Transaction sub-Engine.

## Transactions

Transactions are built on MongoDB sessions and `WithTransaction`. Returning nil from the callback triggers an automatic commit; returning an error triggers an automatic rollback:

```go
err := engine.Transaction(ctx, func(sc context.Context, tx *yukino_orm.Engine) error {
    if _, err := tx.Collection("accounts").
        Where("_id", fromID).
        Update(sc, bson.M{"$inc": bson.M{"balance": -amount}}); err != nil {
        return err
    }
    if _, err := tx.Collection("accounts").
        Where("_id", toID).
        Update(sc, bson.M{"$inc": bson.M{"balance": amount}}); err != nil {
        return err
    }
    return nil
})
```

Important notes:

- Queries made through the `tx` sub-Engine automatically join the transaction
  session, even when a plain context is passed instead of `sc`. Prefer `sc`
  regardless -- it carries the correct deadline/cancellation semantics.
- Do not retain `tx` outside the callback; its session ends when the callback
  returns.
- Transactions require a MongoDB replica set or sharded cluster deployment. They will fail on standalone instances.

## Auto-Increment Sequences

The classic counters collection pattern. Each call returns a globally incrementing `int64`:

```go
id, err := engine.NextSequence(ctx, "order_id")  // returns 1, then 2, 3, ...
```

Under the hood this uses `FindOneAndUpdate` with `$inc` and upsert. Atomicity is guaranteed by MongoDB. Each sequence name corresponds to a separate document in the `counters` collection.

## Logging

The module provides a colored, level-controlled logger:

```go
yukino_orm.Info("connecting to mongo")
yukino_orm.Infof("matched %d users", n)
yukino_orm.Error("connection lost")
yukino_orm.Errorf("query failed: %v", err)

yukino_orm.SetLevel(yukino_orm.InfoLevel)   // all logs enabled (most verbose)
yukino_orm.SetLevel(yukino_orm.ErrorLevel)  // errors only
yukino_orm.SetLevel(yukino_orm.Disabled)    // all logs suppressed
```

Numeric ordering: `InfoLevel (0) < ErrorLevel (1) < Disabled (2)`. Higher values suppress more output.

## Error Handling

- Missing collection before query execution: returns `yukino_orm.ErrCollectionRequired`
- `NewEngine` validation failures: returns `"mongo uri is required"` or `"mongo database is required"`
- All other errors: the original driver error is returned transparently; use `errors.Is(err, mongo.ErrNoDocuments)` or similar for structured handling

It is recommended that callers build centralized error-wrapping for common cases (no matching document, duplicate key, context timeout).

## File Structure

| File                 | Responsibility                                                                                      |
| -------------------- | --------------------------------------------------------------------------------------------------- |
| `engine.go`          | Engine type, connection lifecycle, Collection, Model, Transaction, NextSequence                     |
| `query.go`           | Query type and all chainable condition / sort / pagination / projection methods                     |
| `query_exec.go`      | Terminal methods: Insert, First, Find, Update, Delete, Count, Exists, EnsureIndexes, DropCollection |
| `query_aggregate.go` | Aggregation methods: Sum, Avg, Min, Max, Distinct, CountDistinct, Pluck                             |
| `query_group.go`     | Grouped aggregation: GroupBy, Having, CountAs/SumAs/AvgAs/MinAs/MaxAs, Aggregate                    |
| `query_stream.go`    | Streaming: Cursor, Each                                                                             |
| `filter.go`          | Translation of the condition chain into a `bson.M` filter document                                  |
| `naming.go`          | Struct name to collection name conversion (snake_case + pluralization)                              |
| `log.go`             | Colored, level-controlled logger                                                                    |
| `yukino_orm.go`      | Package declaration                                                                                 |
| `yukino_orm_test.go` | Unit and integration tests                                                                          |

## Usage Notes

1. Update and Delete without any Where conditions will affect the entire collection. Ensure at least one condition is present at the application layer.
2. `Query` is a mutable, stateful struct. To branch multiple query variants from a common base, use `Clone()`.
3. Calling `Select` multiple times replaces the previous projection. Inclusion is the default; prefix a field with `-` to exclude it (MongoDB only allows mixing inclusion with `-_id`).
4. Sum / Avg return 0 for missing or non-numeric fields; Min / Max on non-numeric fields fail at decode.
5. Struct updates are wrapped in `$set` with all marshaled fields, including zero values (honoring `omitempty` bson tags). Use `bson.M` for partial updates.
6. A `Where` added after an `OrWhere` still joins the main AND chain: `Where(a).OrWhere(b).Where(c)` means `(a AND c) OR b`.

## Running Tests

```bash
# Connects to mongodb://localhost:27017 by default
cd yukino_orm
go test ./...

# Custom MongoDB URI
MONGO_URI=mongodb://user:pass@host:27017 go test ./...
```

The test suite creates a timestamp-isolated temporary database for each test case and drops it automatically on cleanup. If the target MongoDB instance requires authentication but no credentials are provided, integration tests will be skipped (not failed), making them CI-friendly.

## Knex.js Alignment

Implemented:

- `where / andWhere / whereNot / orWhere` (equality, operator, and object forms)
- `whereIn / whereNotIn / whereNull / whereNotNull / whereBetween / whereNotBetween`
- `whereLike / whereILike` (SQL LIKE patterns via `$regex`)
- `orWhereIn / orWhereNotIn / orWhereNull / orWhereNotNull / orWhereBetween / orWhereNot`
- `orderBy / limit / offset / select` (with `-field` exclusion)
- `insert (incl. array form) / update (returns matched count) / del / first / find / count / pluck / distinct / countDistinct`
- `increment / decrement`
- upsert (`onConflict().merge()` equivalent via `Upsert`)
- `min / max / sum / avg`
- `groupBy / having` (grouped aggregation via `GroupBy` + `CountAs`/`SumAs`/... + `Aggregate`)
- Streaming (`Cursor` / `Each`, knex `.stream()` equivalent)
- `clone`
- `transaction` (with automatic session binding)

Not yet implemented (planned):

- Callback-based grouping `where(qb => { ... })`
- Generics-based typed query

## License

Governed by the main repository license. See the `LICENSE` file in the repository root.

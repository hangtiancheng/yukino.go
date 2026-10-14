---
name: yukino-orm
description: >
  Knex-style chainable MongoDB ORM for Go (module github.com/hangtiancheng/yukino.go/libs/yukino_orm).
  Use when Go code calls yukino_orm.NewEngine, the Engine methods
  Collection/Model/Client/Database/DatabaseName/Close/DropDatabase/Transaction/NextSequence, the
  chainable Query builder (Where variants WhereNot/WhereIn/WhereNotIn/WhereNull/WhereNotNull/WhereBetween/WhereNotBetween/WhereLike/WhereILike,
  OrWhere variants, OrderBy/Limit/Offset/Select/Clone), terminal methods
  Insert/First/Find/Update/Upsert/Increment/Decrement/Delete/Count/Exists/EnsureIndexes/DropCollection
  with InsertResult/UpsertResult, aggregation Sum/Avg/Min/Max/Distinct/CountDistinct/Pluck, grouped
  aggregation GroupBy/Having/CountAs/SumAs/AvgAs/MinAs/MaxAs/Aggregate, streaming
  Query.Cursor/Query.Each/Cursor methods, or CollectionName/ErrNotFound/ErrCollectionRequired and
  logging helpers SetLevel/Info/Infof/Error/Errorf. Do NOT use for GORM, sqlx, ent, raw mongo-driver
  code, or non-MongoDB datastores.
---

# yukino_orm

| Fact               | Value                                                                                                               |
| ------------------ | ------------------------------------------------------------------------------------------------------------------- |
| Module             | `github.com/hangtiancheng/yukino.go/libs/yukino_orm` (source root `libs/yukino_orm/`, flat layout, no sub-packages) |
| Go directive       | `go 1.26.0`                                                                                                         |
| Latest release tag | `libs/yukino_orm/v0.0.2`                                                                                            |

Knex-inspired chainable query builder over MongoDB, built directly on
`go.mongodb.org/mongo-driver/v2` v2.9.1. Core semantics: same-field conditions
AND-combine without silent overwrites; invalid builder input is recorded and
surfaced at execution rather than panicking; plain update documents auto-wrap in
`$set`; `Update` returns the matched count (Knex "affected rows"). `Engine` owns
connection, session, and sequences; `Query` is a mutable chainable builder with
terminal, aggregation, grouping, and streaming methods; `Cursor` wraps the
driver cursor. Exported types: `Engine`, `Query`, `Cursor`, `InsertResult`,
`UpsertResult`.

## Engine (libs/yukino_orm/engine.go)

`Engine` has no exported fields (`client *mongo.Client`, `database
*mongo.Database`, `databaseName string`, `session *mongo.Session` — the last
non-nil only on a Transaction sub-Engine) and no option type: `NewEngine` only
applies `options.Client().ApplyURI(uri)`; pool size, read preference, write
concern, TLS, and monitors must go into the URI.

| Symbol       | Signature                                                                                                | Behavior                                                                                                                                                                                                                                                                                                                                                                               |
| ------------ | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| NewEngine    | `func NewEngine(ctx context.Context, uri string, database string) (*Engine, error)`                      | Requires non-blank `uri`/`database` after `TrimSpace` (errors `"mongo uri is required"`, `"mongo database is required"`); `mongo.Connect` then `client.Ping(ctx, nil)`; on ping failure disconnects before returning. ctx used only for Connect/Ping, not retained                                                                                                                     |
| Client       | `func (e *Engine) Client() *mongo.Client`                                                                | Driver client; nil-receiver safe (returns nil)                                                                                                                                                                                                                                                                                                                                         |
| Database     | `func (e *Engine) Database() *mongo.Database`                                                            | Handle; nil-receiver safe (returns nil)                                                                                                                                                                                                                                                                                                                                                |
| DatabaseName | `func (e *Engine) DatabaseName() string`                                                                 | Name as passed to `NewEngine`; nil-receiver safe (returns `""`)                                                                                                                                                                                                                                                                                                                        |
| Collection   | `func (e *Engine) Collection(name string) *Query`                                                        | Always non-nil `*Query`; collection bound only when engine, database, and trimmed `name` are all non-empty, else execution methods return `ErrCollectionRequired`                                                                                                                                                                                                                      |
| Model        | `func (e *Engine) Model(value any) *Query`                                                               | Exactly `e.Collection(CollectionName(value))`; a non-struct yields `""` and an unbound `Query`                                                                                                                                                                                                                                                                                         |
| Close        | `func (e *Engine) Close(ctx context.Context) error`                                                      | `client.Disconnect(ctx)`; nil engine/client returns nil                                                                                                                                                                                                                                                                                                                                |
| DropDatabase | `func (e *Engine) DropDatabase(ctx context.Context) error`                                               | `database.Drop(ctx)`; nil engine/database returns nil                                                                                                                                                                                                                                                                                                                                  |
| NextSequence | `func (e *Engine) NextSequence(ctx context.Context, name string) (int64, error)`                         | `FindOneAndUpdate` on hard-coded `counters` collection keyed by `_id: name`, update `$inc: {value: int64(1)}`, `SetUpsert(true)`, `SetReturnDocument(options.After)`; first call returns 1. Joins an active session. Plain errors: `"engine is not initialized"`, `"sequence name is required"`                                                                                        |
| Transaction  | `func (e *Engine) Transaction(ctx context.Context, fn func(sc context.Context, tx *Engine) error) error` | `StartSession`, defers `EndSession(ctx)`, runs `fn` inside `session.WithTransaction`; `tx` is a sub-Engine sharing client/database/name and holding the session; nil return commits, error aborts; `"engine is not initialized"` on nil engine/client; `tx` and any `Query`/`Cursor` derived from it are invalid after the callback returns. Requires a replica set or sharded cluster |

## Query builder (libs/yukino_orm/query.go)

No exported fields and no exported constructor: obtain a `*Query` from
`Engine.Collection`, `Engine.Model`, or `Query.Clone`. Every chainable method
mutates the receiver in place and returns the same pointer (no copy-on-write).

Condition methods (AND chain):

| Method          | Signature                                                                 | BSON produced                                                                                                                                                                                                                                                  |
| --------------- | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Where           | `func (q *Query) Where(args ...any) *Query`                               | 1 arg: `bson.M`/`map[string]any` equality per key, keys processed in `sort.Strings` order, nil values become null checks. 2 args: `(field, value)` equality; nil value becomes null check. 3 args: `(field, op, value)`. Invalid input records a builder error |
| WhereNot        | `func (q *Query) WhereNot(field string, value any) *Query`                | `{field: {$ne: value}}`                                                                                                                                                                                                                                        |
| WhereIn         | `func (q *Query) WhereIn(field string, values any) *Query`                | `{field: {$in: values}}`; `values` stored by reference                                                                                                                                                                                                         |
| WhereNotIn      | `func (q *Query) WhereNotIn(field string, values any) *Query`             | `{field: {$nin: values}}`; stored by reference                                                                                                                                                                                                                 |
| WhereNull       | `func (q *Query) WhereNull(field string) *Query`                          | `{field: nil}`; null equality also matches missing fields                                                                                                                                                                                                      |
| WhereNotNull    | `func (q *Query) WhereNotNull(field string) *Query`                       | `{field: {$ne: nil}}`                                                                                                                                                                                                                                          |
| WhereBetween    | `func (q *Query) WhereBetween(field string, low any, high any) *Query`    | `{field: {$gte: low, $lte: high}}`, inclusive                                                                                                                                                                                                                  |
| WhereNotBetween | `func (q *Query) WhereNotBetween(field string, low any, high any) *Query` | `{field: {$not: {$gte: low, $lte: high}}}`                                                                                                                                                                                                                     |
| WhereLike       | `func (q *Query) WhereLike(field string, pattern string) *Query`          | `{field: {$regex: bson.Regex{Pattern: "^...$"}}}` via `likeToRegex`; case-sensitive                                                                                                                                                                            |
| WhereILike      | `func (q *Query) WhereILike(field string, pattern string) *Query`         | Same with `bson.Regex` `Options: "i"`                                                                                                                                                                                                                          |

Or-branch methods: `OrWhere` (same forms as `Where`; an empty map is a no-op),
`OrWhereNot`, `OrWhereIn`, `OrWhereNotIn`, `OrWhereNull`, `OrWhereNotNull`,
`OrWhereBetween` — each appends exactly one single-condition `$or` branch. No
`OrWhereLike`/`OrWhereILike`/`OrWhereNotBetween` exist; use
`OrWhere(field, "like", pattern)` or `OrWhere(field, "not between", []any{lo, hi})`.

| Method  | Signature                                                           | Behavior                                                                                                                                                                                                                       |
| ------- | ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| OrderBy | `func (q *Query) OrderBy(field string, direction ...string) *Query` | Appends `bson.E` to `sort`, preserving call order; `direction[0]` trimmed, `strings.EqualFold` against `"desc"` gives -1, anything else (including absent) gives 1                                                             |
| Limit   | `func (q *Query) Limit(n int64) *Query`                             | Stores `n`; execution applies it only when `n > 0`, so 0/negative mean "no limit"                                                                                                                                              |
| Offset  | `func (q *Query) Offset(n int64) *Query`                            | Stores `n`; applied only when `n > 0`                                                                                                                                                                                          |
| Select  | `func (q *Query) Select(fields ...string) *Query`                   | Replaces the projection list wholesale by assigning the variadic slice directly, so `Select(mySlice...)` aliases `mySlice`; `-` prefix excludes                                                                                |
| Clone   | `func (q *Query) Clone() *Query`                                    | Nil receiver returns nil. Copies `limit`, `skip`, `err` and reallocates `conditions`, every `orGroups` branch, `sort`, `fields`, `groupFields`, `havingConds`, `aggSpecs`; shares `collection` and `engine` pointers by design |

## Terminal methods (libs/yukino_orm/query_exec.go)

| Method         | Signature                                                                                          | Behavior                                                                                                                                                                                                                                                                            |
| -------------- | -------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Insert         | `func (q *Query) Insert(ctx context.Context, documents ...any) (InsertResult, error)`              | Single slice/array argument expanded per element (`bson.D` and `[]byte`-kind elements excluded). 1 document uses `InsertOne`, 2+ use `InsertMany`. Partial `InsertMany` failure returns the inserted IDs alongside the error. Zero documents: `"at least one document is required"` |
| First          | `func (q *Query) First(ctx context.Context, out any) error`                                        | `FindOne` honoring sort, offset, projection; `Limit` is ignored. Returns `ErrNotFound` on no match                                                                                                                                                                                  |
| Find           | `func (q *Query) Find(ctx context.Context, out any) error`                                         | `Find` + `cursor.All(ctx, out)`; internal cursor closed via defer. Honors sort, limit, offset, projection                                                                                                                                                                           |
| Update         | `func (q *Query) Update(ctx context.Context, update any) (int64, error)`                           | `UpdateMany`; returns `MatchedCount`. Plain documents auto-wrapped in `$set` by `normalizeUpdate`                                                                                                                                                                                   |
| Upsert         | `func (q *Query) Upsert(ctx context.Context, update any) (UpsertResult, error)`                    | `UpdateMany` with `SetUpsert(true)`; on no match MongoDB derives the new document from filter equalities plus the update                                                                                                                                                            |
| Increment      | `func (q *Query) Increment(ctx context.Context, field string, amount ...int64) (int64, error)`     | `Update(ctx, bson.M{"$inc": bson.M{field: n}})`; `n` defaults to 1; matched count                                                                                                                                                                                                   |
| Decrement      | `func (q *Query) Decrement(ctx context.Context, field string, amount ...int64) (int64, error)`     | `Update` with `$inc: {field: -n}`; matched count                                                                                                                                                                                                                                    |
| Delete         | `func (q *Query) Delete(ctx context.Context) (int64, error)`                                       | `DeleteMany`; returns `DeletedCount`                                                                                                                                                                                                                                                |
| Count          | `func (q *Query) Count(ctx context.Context) (int64, error)`                                        | `CountDocuments(filter)`; `OrderBy`, `Limit`, `Offset` ignored                                                                                                                                                                                                                      |
| Exists         | `func (q *Query) Exists(ctx context.Context) (bool, error)`                                        | `Count(ctx) > 0`; the error comes back with the bool, so check it                                                                                                                                                                                                                   |
| EnsureIndexes  | `func (q *Query) EnsureIndexes(ctx context.Context, indexes []mongo.IndexModel) ([]string, error)` | `Indexes().CreateMany`; `(nil, nil)` for empty `indexes`, after preflight                                                                                                                                                                                                           |
| DropCollection | `func (q *Query) DropCollection(ctx context.Context) error`                                        | `collection.Drop`                                                                                                                                                                                                                                                                   |

```go
type InsertResult struct { InsertedIDs []any; InsertedCount int64 }
type UpsertResult struct { MatchedCount int64; ModifiedCount int64; UpsertedCount int64; UpsertedID any }
```

`InsertedIDs` are driver-reported `_id`s; `InsertedCount` is `len(InsertedIDs)`
computed locally, not server-reported. `UpsertResult` is a straight copy of the
driver's `*mongo.UpdateResult`, the only place `ModifiedCount` is exposed.

## Aggregation (libs/yukino_orm/query_aggregate.go)

| Method                | Signature                                                                                             | Behavior                                                                                                                                                                                                                                                                                                                  |
| --------------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Sum / Avg / Min / Max | `func (q *Query) Sum(ctx context.Context, field string) (float64, error)` (same shape for the others) | Pipeline `[{$match: filter}, {$group: {_id: nil, result: {$sum/$avg/$min/$max: "$field"}}}]`; `(0, nil)` when nothing matched; sort/limit/offset ignored; Min/Max over non-numeric values fail decoding into `float64`                                                                                                    |
| Distinct              | `func (q *Query) Distinct(ctx context.Context, field string) ([]any, error)`                          | `collection.Distinct(ctx, field, filter)`; caller type-asserts; sort/limit/offset ignored; command reply subject to MongoDB's 16 MB BSON limit                                                                                                                                                                            |
| CountDistinct         | `func (q *Query) CountDistinct(ctx context.Context, field string) (int64, error)`                     | `int64(len(Distinct(...)))`; materializes all distinct values client-side                                                                                                                                                                                                                                                 |
| Pluck                 | `func (q *Query) Pluck(ctx context.Context, field string, out any) error`                             | Streams with projection `{field: 1, "_id": 0}` (`_id: 0` omitted when field is `"_id"`); `out` must be a non-nil pointer to a slice; blank `field` errors; honors sort/limit/offset; dotted paths split on `.`; documents lacking the field contribute the element zero value; does not mutate the query's `Select` state |

## Grouped aggregation (libs/yukino_orm/query_group.go)

| Method                        | Signature                                                               | Behavior                                                                                                                                                                                                   |
| ----------------------------- | ----------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GroupBy                       | `func (q *Query) GroupBy(fields ...string) *Query`                      | Appends group keys; a blank field records a builder error and appends nothing from that call. Keys surface in result rows under their flattened name (dots become underscores: `addr.city` to `addr_city`) |
| Having                        | `func (q *Query) Having(args ...any) *Query`                            | Same argument forms as `Where`; field names must be result columns (flattened keys or aliases)                                                                                                             |
| CountAs                       | `func (q *Query) CountAs(alias string) *Query`                          | Per-group `{$sum: 1}` under `alias`                                                                                                                                                                        |
| SumAs / AvgAs / MinAs / MaxAs | `func (q *Query) SumAs(field string, alias string) *Query` (same shape) | Per-group `{$sum/$avg/$min/$max: "$field"}`; note field first, alias second; blank field records a builder error                                                                                           |
| Aggregate                     | `func (q *Query) Aggregate(ctx context.Context, out any) error`         | `preflightBase` (pending group state allowed here only), build pipeline, `collection.Aggregate`, `cursor.All(ctx, out)`; `out` is a pointer to a slice                                                     |

Call-time alias validation (builder error): alias non-blank, not `_id`, no
leading `$`, no `.`. Pipeline-time validation (returned by `Aggregate`): `GroupBy`
required; `Select` cannot combine with `GroupBy`; flattened keys unique; aliases
must not collide; every `Having` field and `OrderBy` key must be a known result
column. Pipeline emitted: `$match(Where)` (omitted if empty) to `$group` to
`$project` (`_id: 0`, flattened keys from `$_id` or `$_id.flatKey`, aliases kept)
to `$match(Having)` (omitted if empty) to `$sort` to `$skip` to `$limit`; exactly
7 stages when fully populated. Ordering/pagination apply to grouped rows; `Where`
filters documents, `Having` filters rows.

## Streaming (libs/yukino_orm/query_stream.go)

| Method | Signature                                                                   | Behavior                                                                                                                                                                                 |
| ------ | --------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Cursor | `func (q *Query) Cursor(ctx context.Context) (*Cursor, error)`              | `Find` with the same options as `Find`; caller owns the cursor and must `defer cursor.Close(ctx)`, or the server-side cursor leaks until the server idle timeout (10 minutes by default) |
| Each   | `func (q *Query) Each(ctx context.Context, fn func(c *Cursor) error) error` | Opens a cursor, defers `Close`, calls `fn` per document (it receives the `*Cursor`, not a decoded value); first `fn` error stops iteration and is returned, otherwise `cursor.Err()`     |

`Cursor` methods: `Next(ctx) bool`, `Decode(out any) error` (no session binding,
no I/O), `Current() bson.Raw` (valid until next `Next`), `Err() error`,
`Close(ctx) error`; `Next`/`Close` bind the engine session when present. A
zero-value `Cursor{}` panics on every method; only cursors from `Cursor`/`Each`
are valid.

## Filter DSL (libs/yukino_orm/filter.go)

Accepted `Where` forms (verified against `parseWhere`):

```go
q.Where(bson.M{"a": 1, "b": nil})   // {a: 1, b: nil}     keys sorted; nil is a null check
q.Where("age", 18)                  // {age: 18}
q.Where("age", ">=", 18)            // {age: {$gte: 18}}
q.Where("tags", "in", []string{"x"})// {tags: {$in: ["x"]}}
q.Where("name", "like", "Tom%")     // {name: {$regex: /^Tom.*$/}}
q.Where("age", "between", []int{1, 9})     // {age: {$gte: 1, $lte: 9}}
q.Where("age", "not between", []int{1, 9}) // {age: {$not: {$gte: 1, $lte: 9}}}
q.Where("meta", "$exists", true)    // {meta: {$exists: true}} raw $-ops pass through
```

Rejected forms record a builder error surfaced at the next execution call:
`Where()` (wrong arity), `Where(123, "x")` (field must be a non-empty string),
`Where("f", 1, 2)` (operator must be a string), `Where("f", "bogus", 1)` (unsupported
operator), `Where("f", "like", 42)` (like requires a string), `Where("f", "between", 5)`
(needs a 2-element slice or array), `Where([]string{"a"})` (single argument must be
`bson.M` or `map[string]any`).

Operator aliases resolve after lowercasing and trimming: `=`/`==` equality,
`!=`/`<>` `$ne`, `>`/`>=`/`<`/`<=`, `in` `$in`, `not in`/`nin` `$nin`, `like`,
`ilike`, `between` (accepts `[2]any` or any length-2 slice or array via
reflection), `not between`. An unrecognized `$`-prefixed operator passes through
verbatim using the raw (untrimmed, unlowercased) key; any other unrecognized
operator records a builder error.

`likeToRegex`: `regexp.QuoteMeta`, then `%` to `.*`, `_` to `.`, anchored: `"tom"`
to `^tom$`, `"%tom%"` to `^.*tom.*$`, `"to_m"` to `^to.m$`, `"a.b%"` to `^a\.b.*$`,
`"100%"` to `^100.*$`. No escape syntax (literal `%`/`_` unmatched); `WhereLike`
is a full match unless you write `%...%`.

AND composition (`buildConditionFilter`): builder-created operator maps merge
new ops (equality merges as `$eq`); a user equality value that happens to be a
`bson.M` sub-document is never mutated; anything unmergeable (duplicate
equality, operator after equality, duplicate operator) goes to a top-level `$and`
array; no condition is ever dropped or overwritten. Verified:

```go
Where("age", ">", 18).Where("age", "<", 30)  // {age: {$gt: 18, $lt: 30}}
Where("age", ">", 10).Where("age", 18)       // {age: {$gt: 10, $eq: 18}}
Where("age", 18).Where("age", ">", 10)       // {age: 18, $and: [{age: {$gt: 10}}]}
Where("a", 1).Where("a", 2)                  // {a: 1, $and: [{a: 2}]}
```

Or composition (`buildFilter`): no or-groups yields just the condition fold;
otherwise the main chain becomes a branch only when non-empty, each non-empty
or-group becomes a branch, a single branch is returned unwrapped, two or more
become `{$or: [...]}`. An OrWhere-only query never matches everything, and a
`Where` added after an `OrWhere` joins the main AND chain:
`Where(a).OrWhere(b).Where(c)` means `(a AND c) OR b`. `buildProjection`: nil
when no `Select` fields; `-name` maps to 0, else 1.

## CollectionName (libs/yukino_orm/naming.go)

`func CollectionName(value any) string`: unwrap pointers; one slice/array level
(`[][]User` yields `[]User`, not a struct, so `""`); require `reflect.Struct`
(else `""`); return `pluralize(toSnake(typ.Name()))`. `toSnake` inserts `_` before
every uppercase ASCII letter past index 0 (no acronym handling: `HTTPServer` to
`h_t_t_p_servers`). `pluralize`: consonant+`y` to `ies`, `s`/`x`/`sh`/`ch` to `es`,
else `s`; irregulars unhandled (`Person` to `persons`). Anonymous structs have an
empty `Name()`, so `CollectionName(struct{}{})` returns `""` and yields an unbound
`Query`. Tests verify `&testUser{}` to `test_users`, `[]testCity{}` to `test_cities`,
`42` to `""`.

`naming.go` implements only collection naming. Document field naming is the
driver's BSON codec: untagged fields store fully lowercased (`CreatedAt` to
`createdat`, not `created_at`); the primary key is `_id` only when tagged
`bson:"_id"`; embedded structs flatten only with `bson:",inline"`; `omitempty`
drops zero fields from a struct update's `$set`. Tag every field; there is no
override hook — call `engine.Collection("explicit_name")` instead.

## Logging (libs/yukino_orm/log.go)

```go
const ( InfoLevel = iota; ErrorLevel; Disabled ) // 0, 1, 2
var ( Error, Errorf = errorLogger.Println, errorLogger.Printf // os.Stderr, red "[error]" prefix
      Info, Infof   = infoLogger.Println, infoLogger.Printf )  // os.Stdout, blue "[info ]" prefix
func SetLevel(level int) // mutex-guarded; ErrorLevel errors only, Disabled silences both
```

Both loggers use `log.LstdFlags|log.Lshortfile`. `SetLevel` resets both to
stdout, restores the error logger to stderr, then discards any logger below the
requested level; values above `Disabled` behave like `Disabled`. The ORM itself
never calls these helpers (no query logging); they serve application code.

## Errors (libs/yukino_orm/query_exec.go)

```go
var ErrCollectionRequired = errors.New("collection is required before query execution")
var ErrNotFound = mongo.ErrNoDocuments // variable alias; errors.Is matches both names
```

`ErrCollectionRequired` is returned by every execution method on a nil receiver
or unbound collection (`engine.Collection("")`, `engine.Model(42)`, nil engine);
`ErrNotFound` by `First` on no match. No other package error is a sentinel:
`NewEngine`/`NextSequence`/`Transaction`/`Insert` return plain `errors.New` values
(`"mongo uri is required"`, `"mongo database is required"`, `"engine is not
initialized"`, `"sequence name is required"`, `"at least one document is
required"`), builder errors are unwrapped `fmt.Errorf`/`errors.New` values, and
driver errors come back verbatim (use `mongo.IsDuplicateKeyError` etc.).

## Implementation semantics

- **Builder mutation versus cloning.** Reusing one `*Query` for two variants
  applies the first variant's conditions to the second:

  ```go
  base := engine.Collection("users").Where("age", ">=", 18)
  withEmail, _ := base.WhereNotNull("email").Count(ctx) // WRONG: mutates base
  total, _ := base.Count(ctx) // sees the email condition too
  withEmail, _ = base.Clone().WhereNotNull("email").Count(ctx) // CORRECT
  total, _ = base.Clone().Count(ctx)
  ```

  Never stash a partially built `Query` on a shared struct. `Clone` copies
  slices but not the values inside them: `Select(mySlice...)` aliases the
  caller's slice and condition values such as `WhereIn`'s slice are held by
  reference, mutating them before execution changes the filter of the original
  and every clone.

- **Builder error accumulation.** `setErr` keeps only the first error; parse
  errors come from `Where`, `OrWhere`, `Having`; blank-field errors from `GroupBy`;
  alias/field errors from the `*As` methods. The direct-append methods
  (`WhereNot`, `WhereIn`, `WhereNotIn`, `WhereNull`, `WhereNotNull`,
  `WhereBetween`, `WhereNotBetween`, `WhereLike`, `WhereILike`, all `OrWhere*`
  variants) validate nothing: a blank field silently keys the filter on `""`.
- **Preflight order.** `preflightBase`: nil receiver to `ErrCollectionRequired`,
  then `q.err`, then nil collection to `ErrCollectionRequired`; a builder error
  therefore outranks the missing collection. `preflight` adds rejection of pending
  group state (`"GroupBy/Having/aggregation aliases are only supported by
Aggregate"`) for every execution method except `Aggregate`; `Increment`,
  `Decrement`, `Exists`, `CountDistinct` inherit it via delegation.
- **Update normalization and insert expansion.** `normalizeUpdate` wraps a document
  with no `$`-prefixed key in `{$set: doc}`: `bson.M`, `map[string]any`, `bson.D`
  (scanned key by key), and any struct (after unwrapping non-nil pointers). Operator
  documents, nil pointers, and other types pass through. An empty `bson.M{}` wraps
  to `{$set: {}}`, which the server rejects. `expandInsertDocs` acts only on a
  single argument: one `bson.D`, a non-slice, or a byte-kind slice stays as-is,
  else the slice/array is flattened per-element.
- **Transaction session propagation.** Every execution path calls `Query.execCtx`,
  which calls `Engine.sessionContext`: when the engine holds a session and the
  ctx does not already carry one (`mongo.SessionFromContext`), the session is
  bound via `mongo.NewSessionContext`. Queries through the `tx` sub-Engine join
  the transaction even with the plain outer ctx; prefer `sc` anyway for deadline
  and cancellation semantics. `NextSequence` and `Cursor.Next`/`Close` bind the
  same way. `Transaction` defers `EndSession`, so a captured `tx`, `Query`, or
  `Cursor` is invalid after the callback.
- **Concurrency.** The package's only lock is the `sync.Mutex` guarding `SetLevel`.
  `*Engine` is safe for concurrent use (fields written only at construction; share
  one process-wide). The transaction sub-`Engine` is not (a `mongo.Session` rejects
  concurrent operations). `*Query` is not: chain building appends without guards;
  give each goroutine its own `Clone`. `*Cursor` is not. The `Error`/`Info` package
  vars are plain vars: reassigning them concurrently with calls is a race.
- **Contexts and zero values.** No `context.WithTimeout` in the package; caller
  deadlines go straight to the driver. At shutdown, `Engine.Close` and
  `DropDatabase` need a context that is not already cancelled. `&Query{}` builds
  and inspects filters but executes to `ErrCollectionRequired` (how unit tests
  run without a server). A nil or zero `*Engine`: accessors return zero values,
  `Collection`/`Model` return unbound queries, `Close`/`DropDatabase` are nil
  no-ops, `NextSequence`/`Transaction` return `"engine is not initialized"`. A
  nil `*Query`: `Clone` returns nil, execution returns
  `ErrCollectionRequired`. `OrderBy(f)` is ascending; `Increment`/`Decrement`
  default to 1; `Limit(0)`/`Offset(0)` apply nothing.

## Usage

```go
engine, err := yukino_orm.NewEngine(ctx, "mongodb://localhost:27017", "my-app")
if err != nil { log.Fatal(err) } // Close with a context that is not already cancelled

// Insert: variadic documents, or one slice argument (auto-expanded).
res, err := engine.Collection("users").Insert(ctx, []*User{
    {ID: 1, Name: "Alice", Age: 30}, {ID: 2, Name: "Bob", Age: 25},
}) // res.InsertedCount, res.InsertedIDs

var adults []User
err = engine.Model(&User{}). // "users"
    Where("age", ">=", 18).WhereNotNull("email").WhereLike("name", "A%").
    OrderBy("created_at", "desc").Limit(10).Offset(0).
    Select("name", "email", "-_id").Find(ctx, &adults)

matched, err := engine.Collection("users").Where("_id", int64(1)).
    Update(ctx, bson.M{"name": "Alice Updated"}) // $set-wrapped; matched count
err = engine.Collection("users").OrderBy("name").Pluck(ctx, "name", &names)

// Grouped aggregation: Where filters documents, Having filters rows.
type cityAgg struct {
    City  string  `bson:"city"`
    N     int64   `bson:"n"`
    Total float64 `bson:"total"`
}
var rows []cityAgg
err = engine.Collection("orders").Where("status", "paid").
    GroupBy("city").CountAs("n").SumAs("amount", "total"). // field first, alias second
    Having("n", ">=", 2).OrderBy("total", "desc").Limit(10).Aggregate(ctx, &rows)

// Streaming. Each closes for you; manual Cursor requires defer cursor.Close(ctx).
err = engine.Collection("orders").Each(ctx, func(c *yukino_orm.Cursor) error {
    var o Order
    if err := c.Decode(&o); err != nil { return err }
    return nil // returning an error stops iteration
})

// Transaction (replica set required); tx queries auto-join the session.
err = engine.Transaction(ctx, func(sc context.Context, tx *yukino_orm.Engine) error {
    _, err := tx.Collection("users").Where("_id", int64(1)).Update(sc, bson.M{"$inc": bson.M{"age": 1}})
    return err // nil commits, non-nil aborts
})

nextID, err := engine.NextSequence(ctx, "order_id") // counters collection
_, err = engine.Collection("users").EnsureIndexes(ctx, []mongo.IndexModel{{
    Keys:    bson.D{{Key: "email", Value: 1}},
    Options: options.Index().SetUnique(true).SetName("uniq_email"),
}})
```

## Testing (libs/yukino_orm/yukino_orm_test.go)

Unit tests (no server, in-package, drive `&Query{}` by asserting `q.err`,
`q.buildFilter()`, `q.buildProjection()`, `q.buildGroupPipeline()`):
`TestBuildFilter*` (merge, equality/operator preservation, or-without-base,
WhereNull, WhereBetween), `TestWhereOperatorAliases`, `TestWhereObjectForm`,
`TestWhereInvalidArgsReturnErrorNotPanic`, `TestWhereNotBetweenFilter`,
`TestLikeToRegex`, `TestPluralize`, `TestNormalizeUpdate{,Extended}`,
`TestExpandInsertDocs`, `TestClone*` (Independence, CopiesGroupState),
`TestBuildProjectionExclusion`, `TestOrderByCaseInsensitive`, `TestGroup*`
(PipelineShape, BuilderValidation, ResultColumnValidation,
FlattenedKeyCollision), `TestSingleDottedGroupKeyProjection`,
`TestAggAliasSyntaxValidation`, `TestOrWhereEmptyMapIsNoop`, `TestCollectionName`,
`TestNewEngineValidation`.

Integration tests use the `openTestEngine(t)` harness: URI from `MONGO_URI`
(default `mongodb://localhost:27017/`), 10s context, database
`yukino_orm_test_<UnixNano>` per test, a `{create: "__access_check"}` probe
skipping via `t.Skipf` when the error contains `"Unauthorized"`, cleanup drops
the database and closes the engine with fresh 10s contexts. A `NewEngine`
failure is a hard `t.Fatalf`: with no reachable server integration tests fail
rather than skip. `TestTransactionAutoSessionBinding` skips when the error text
contains `"Transaction numbers"` or `"IllegalOperation"` (standalone mongod
rejection); it deliberately passes the outer ctx to prove auto session binding,
then asserts the rollback.

Unit-only run, from `libs/yukino_orm/`:

```
go test -run 'TestBuild|TestWhere(Object|Operator|Invalid|NotBetween)|TestLike|TestPluralize|TestNormalize|TestExpand|TestClone|TestGroup(Pipeline|Builder|Result|Flattened)|TestOrderBy|TestCollectionName|TestNewEngine|TestSingleDotted|TestAggAlias|TestOrWhereEmptyMap' .
```

## Pitfalls

1. Chaining mutates in place; `Clone()` at every branch point. `Clone` does not
   copy by-reference values: `Select` slices and `WhereIn`-style condition
   values alias into every clone.
2. An empty filter on `Update` or `Delete` targets the whole collection; no guard
   exists, enforce at least one condition in application code.
3. `Where` after `OrWhere` joins the main AND chain: `Where(a).OrWhere(b).Where(c)`
   is `(a AND c) OR b`, not SQL's `a OR (b AND c)`. Put `OrWhere` calls last.
4. Builder errors are deferred to the next execution call; never discard execution
   errors. Direct-append condition methods validate nothing (blank field silently
   keys the filter on `""`).
5. `Update` returns `MatchedCount`, not `ModifiedCount`; an idempotent update
   reports 1; use `Upsert` to know whether a write occurred. `Update` with an
   empty document fails server-side (`$set: {}`); struct updates set every
   marshaled field unless tags carry `omitempty`; prefer `bson.M` for partials.
6. Pending `GroupBy`/`Having`/alias state makes every non-`Aggregate` execution
   fail by design; `Select` cannot combine with `GroupBy`. Grouped
   `Having`/`OrderBy` must name result columns (flattened keys or aliases);
   aliases cannot be `_id`, start with `$`, contain `.`, repeat, or collide.
7. `Sum`/`Avg` return 0 for "nothing matched" and for a genuine zero alike;
   `Min`/`Max` decode into `float64`, so string/date extremes fail; scalar
   aggregation, `Count`, and `Distinct` ignore `OrderBy`/`Limit`/`Offset`
   (`Limit(10).Count(ctx)` returns the full count); `First` honors `Offset`
   but ignores `Limit`.
8. `Distinct` returns `[]any` for caller type-assertion; `CountDistinct`
   materializes every distinct value client-side. `Pluck` needs a non-nil pointer
   to a slice of the value type; missing fields contribute zero values.
9. Raw `$`-prefixed operators pass through without value-shape validation;
   malformed ones surface as server errors. `Query.Cursor` cursors must be
   closed by the caller (10-minute default idle timeout on leak); prefer `Each`.
10. Transactions require a replica set or sharded cluster; the `tx` sub-Engine and
    everything derived from it die when the callback returns and are not
    concurrent-safe. `NextSequence` hard-codes the `counters` collection name.
11. `CollectionName` has no acronym handling, no irregular plurals, and returns
    `""` for non-structs and anonymous structs; use
    `engine.Collection("explicit")` when derivation is wrong.
12. Field names are raw strings matched against stored BSON keys; untagged Go
    fields store fully lowercased (`CreatedAt` to `createdat`); tag every field,
    including `bson:"_id"`.
13. `NewEngine` exposes no client options and pings eagerly; the exported logging
    helpers are never called by the ORM (no query logging; use the driver's own
    monitor by building a client yourself, which bypasses `NewEngine`).

## File map (all under libs/yukino_orm/)

| File                 | Purpose                                                                                                                                                                                                                                                                                                                          |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `yukino_orm.go`      | Package clause only                                                                                                                                                                                                                                                                                                              |
| `engine.go`          | `Engine`, `NewEngine`, `Client`, `Database`, `DatabaseName`, `Collection`, `Model`, `Close`, `DropDatabase`, `NextSequence`, `Transaction`, `sessionContext`                                                                                                                                                                     |
| `query.go`           | `Query` builder state, `setErr`, `Where`/`OrWhere` families, `OrderBy`, `Limit`, `Offset`, `Select`, `Clone`                                                                                                                                                                                                                     |
| `query_exec.go`      | `ErrCollectionRequired`, `ErrNotFound`, `InsertResult`, `UpsertResult`, `Insert`, `First`, `Find`, `Update`, `Upsert`, `Increment`, `Decrement`, `Delete`, `Count`, `Exists`, `EnsureIndexes`, `DropCollection`, `findOptions`, `preflight`, `preflightBase`, `execCtx`, `normalizeUpdate`, `hasOperatorKey`, `expandInsertDocs` |
| `query_aggregate.go` | `Distinct`, `CountDistinct`, `Pluck`, `Sum`, `Avg`, `Min`, `Max`, `aggregate`                                                                                                                                                                                                                                                    |
| `query_group.go`     | `aggSpec`, `GroupBy`, `Having`, `CountAs`, `SumAs`, `AvgAs`, `MinAs`, `MaxAs`, `Aggregate`, `addAggSpec`, `addFieldAggSpec`, `buildGroupPipeline`, `groupKeyName`                                                                                                                                                                |
| `query_stream.go`    | `Cursor` type (`Next`, `Decode`, `Current`, `Err`, `Close`, `bind`), `Query.Cursor`, `Query.Each`                                                                                                                                                                                                                                |
| `filter.go`          | `condition`, `opAliases`, `parseWhere`, `parseWhereMap`, `normalizeOp`, `toBetweenPair`, `likeToRegex`, `buildFilter`, `buildConditionFilter`, `buildProjection`                                                                                                                                                                 |
| `naming.go`          | `CollectionName`, `toSnake`, `pluralize`, `isVowel`                                                                                                                                                                                                                                                                              |
| `log.go`             | `Error`, `Errorf`, `Info`, `Infof`, `InfoLevel`, `ErrorLevel`, `Disabled`, `SetLevel`                                                                                                                                                                                                                                            |
| `yukino_orm_test.go` | Unit tests over `&Query{}` plus integration tests behind `openTestEngine`                                                                                                                                                                                                                                                        |
| `README.md`          | User-facing docs, including a `Knex.js Alignment` section                                                                                                                                                                                                                                                                        |
| `go.mod` / `go.sum`  | Module declaration, `go 1.26.0`, driver requirement, indirect deps, sibling `replace` directives                                                                                                                                                                                                                                 |

## Dependencies

Direct: `go.mongodb.org/mongo-driver/v2` v2.9.1, the only required module,
used throughout: `mongo.Connect`/`Client`/`Database`/`Collection`,
`*mongo.Session` + `mongo.NewSessionContext`, `mongo.Cursor`, `mongo.IndexModel`,
`mongo.ErrNoDocuments` as `ErrNotFound`, `bson.M`/`bson.D`/`bson.A`/`bson.E`/
`bson.Raw`, `bson.Regex` for LIKE, `mongo/options` builders. Standard library
only otherwise: `context`, `errors`, `fmt`, `io`, `log`, `os`, `reflect`,
`regexp`, `sort`, `strings`, `sync`.

Indirect (driver transitive closure, per go.mod): `github.com/davecgh/go-spew`,
`github.com/klauspost/compress`, `github.com/xdg-go/pbkdf2`,
`github.com/xdg-go/scram`, `github.com/xdg-go/stringprep`,
`github.com/youmark/pkcs8`, `golang.org/x/crypto`, `golang.org/x/sync`,
`golang.org/x/text`. `go.mod` also carries `replace` directives pointing
`yukino_cache`, `yukino_http`, and `yukino_rpc` at their sibling directories
under `libs/` (the `yukino_orm` self-replace line is commented out); none is
required, so yukino_orm has no compile-time dependency on any sibling module.
Runtime requirement: a MongoDB server; transactions additionally require a
replica set or sharded cluster.

## Sibling skills

Load `yukino-http` or `yukino-rpc` when wiring an `Engine` into a server process:
one `Engine` at startup, inject it, pass request contexts into query methods so
cancellation and deadlines propagate, and `Engine.Close` with a separate shutdown
context. `*Engine` is concurrency-safe; `*Query` is not. Load `yukino-cache` for
read-through caching of ORM reads: cache decoded result structs, never the
mutable `Query`.

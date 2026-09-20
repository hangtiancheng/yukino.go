// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package yukino_orm

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrCollectionRequired = errors.New("collection is required before query execution")

// ErrNotFound is returned by First when no document matches the filter.
// It aliases mongo.ErrNoDocuments so callers need not import the driver.
var ErrNotFound = mongo.ErrNoDocuments

type InsertResult struct {
	InsertedIDs   []any
	InsertedCount int64
}

type UpsertResult struct {
	MatchedCount  int64
	ModifiedCount int64
	UpsertedCount int64
	UpsertedID    any
}

func (q *Query) Insert(ctx context.Context, documents ...any) (InsertResult, error) {
	if err := q.preflight(); err != nil {
		return InsertResult{}, err
	}
	documents = expandInsertDocs(documents)
	if len(documents) == 0 {
		return InsertResult{}, errors.New("at least one document is required")
	}
	ctx = q.execCtx(ctx)
	if len(documents) == 1 {
		result, err := q.collection.InsertOne(ctx, documents[0])
		if err != nil {
			return InsertResult{}, err
		}
		return InsertResult{InsertedIDs: []any{result.InsertedID}, InsertedCount: 1}, nil
	}
	result, err := q.collection.InsertMany(ctx, documents)
	if err != nil {
		if result != nil {
			return InsertResult{InsertedIDs: result.InsertedIDs, InsertedCount: int64(len(result.InsertedIDs))}, err
		}
		return InsertResult{}, err
	}
	return InsertResult{InsertedIDs: result.InsertedIDs, InsertedCount: int64(len(result.InsertedIDs))}, nil
}

// expandInsertDocs allows Insert(ctx, sliceOfDocs) by flattening a single
// slice/array argument into individual documents. bson.D (a single document
// that happens to be a slice) and byte slices are left untouched.
func expandInsertDocs(documents []any) []any {
	if len(documents) != 1 {
		return documents
	}
	if _, isDoc := documents[0].(bson.D); isDoc {
		return documents
	}
	v := reflect.ValueOf(documents[0])
	if !v.IsValid() || (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) {
		return documents
	}
	if v.Type().Elem().Kind() == reflect.Uint8 {
		return documents
	}
	expanded := make([]any, v.Len())
	for i := 0; i < v.Len(); i++ {
		expanded[i] = v.Index(i).Interface()
	}
	return expanded
}

func (q *Query) First(ctx context.Context, out any) error {
	if err := q.preflight(); err != nil {
		return err
	}
	opts := options.FindOne()
	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if q.skip > 0 {
		opts.SetSkip(q.skip)
	}
	if proj := q.buildProjection(); proj != nil {
		opts.SetProjection(proj)
	}
	return q.collection.FindOne(q.execCtx(ctx), q.buildFilter(), opts).Decode(out)
}

func (q *Query) Find(ctx context.Context, out any) error {
	if err := q.preflight(); err != nil {
		return err
	}
	ctx = q.execCtx(ctx)
	cursor, err := q.collection.Find(ctx, q.buildFilter(), q.findOptions())
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	return cursor.All(ctx, out)
}

func (q *Query) findOptions() *options.FindOptionsBuilder {
	opts := options.Find()
	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if q.limit > 0 {
		opts.SetLimit(q.limit)
	}
	if q.skip > 0 {
		opts.SetSkip(q.skip)
	}
	if proj := q.buildProjection(); proj != nil {
		opts.SetProjection(proj)
	}
	return opts
}

// Update applies the update to all matching documents and returns the number
// of matched documents (knex-style affected rows). Plain documents without
// "$" operators are wrapped in $set.
func (q *Query) Update(ctx context.Context, update any) (int64, error) {
	if err := q.preflight(); err != nil {
		return 0, err
	}
	result, err := q.collection.UpdateMany(q.execCtx(ctx), q.buildFilter(), normalizeUpdate(update))
	if err != nil {
		return 0, err
	}
	return result.MatchedCount, nil
}

// Upsert updates all matching documents, inserting a new document from the
// filter equalities and the update when nothing matches.
func (q *Query) Upsert(ctx context.Context, update any) (UpsertResult, error) {
	if err := q.preflight(); err != nil {
		return UpsertResult{}, err
	}
	opts := options.UpdateMany().SetUpsert(true)
	result, err := q.collection.UpdateMany(q.execCtx(ctx), q.buildFilter(), normalizeUpdate(update), opts)
	if err != nil {
		return UpsertResult{}, err
	}
	return UpsertResult{
		MatchedCount:  result.MatchedCount,
		ModifiedCount: result.ModifiedCount,
		UpsertedCount: result.UpsertedCount,
		UpsertedID:    result.UpsertedID,
	}, nil
}

// Increment atomically adds amount (default 1) to the field on all matching
// documents and returns the number of matched documents.
func (q *Query) Increment(ctx context.Context, field string, amount ...int64) (int64, error) {
	n := int64(1)
	if len(amount) > 0 {
		n = amount[0]
	}
	return q.Update(ctx, bson.M{"$inc": bson.M{field: n}})
}

// Decrement atomically subtracts amount (default 1) from the field on all
// matching documents and returns the number of matched documents.
func (q *Query) Decrement(ctx context.Context, field string, amount ...int64) (int64, error) {
	n := int64(1)
	if len(amount) > 0 {
		n = amount[0]
	}
	return q.Update(ctx, bson.M{"$inc": bson.M{field: -n}})
}

func (q *Query) Delete(ctx context.Context) (int64, error) {
	if err := q.preflight(); err != nil {
		return 0, err
	}
	result, err := q.collection.DeleteMany(q.execCtx(ctx), q.buildFilter())
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}

func (q *Query) Count(ctx context.Context) (int64, error) {
	if err := q.preflight(); err != nil {
		return 0, err
	}
	return q.collection.CountDocuments(q.execCtx(ctx), q.buildFilter())
}

func (q *Query) Exists(ctx context.Context) (bool, error) {
	count, err := q.Count(ctx)
	return count > 0, err
}

func (q *Query) EnsureIndexes(ctx context.Context, indexes []mongo.IndexModel) ([]string, error) {
	if err := q.preflight(); err != nil {
		return nil, err
	}
	if len(indexes) == 0 {
		return nil, nil
	}
	return q.collection.Indexes().CreateMany(q.execCtx(ctx), indexes)
}

func (q *Query) DropCollection(ctx context.Context) error {
	if err := q.preflight(); err != nil {
		return err
	}
	return q.collection.Drop(q.execCtx(ctx))
}

// preflight validates the Query before hitting the driver: a collection must
// be bound, no builder error may be pending, and pending GroupBy/Having state
// must not be silently ignored (it is only consumed by Aggregate).
func (q *Query) preflight() error {
	if err := q.preflightBase(); err != nil {
		return err
	}
	if len(q.groupFields) > 0 || len(q.havingConds) > 0 || len(q.aggSpecs) > 0 {
		return errors.New("GroupBy/Having/aggregation aliases are only supported by Aggregate")
	}
	return nil
}

func (q *Query) preflightBase() error {
	if q == nil {
		return ErrCollectionRequired
	}
	if q.err != nil {
		return q.err
	}
	if q.collection == nil {
		return ErrCollectionRequired
	}
	return nil
}

// execCtx binds the engine's transaction session to ctx so that queries made
// through a Transaction sub-Engine participate in the transaction even when
// the caller passes a plain context instead of the session context.
func (q *Query) execCtx(ctx context.Context) context.Context {
	if q == nil || q.engine == nil {
		return ctx
	}
	return q.engine.sessionContext(ctx)
}

// normalizeUpdate wraps plain documents (bson.M, map, bson.D, struct) that
// contain no "$"-prefixed keys into {$set: doc}, aligning with knex update.
func normalizeUpdate(update any) any {
	switch doc := update.(type) {
	case bson.M:
		if hasOperatorKey(doc) {
			return update
		}
		return bson.M{"$set": doc}
	case map[string]any:
		if hasOperatorKey(doc) {
			return update
		}
		return bson.M{"$set": doc}
	case bson.D:
		for _, e := range doc {
			if strings.HasPrefix(e.Key, "$") {
				return update
			}
		}
		return bson.M{"$set": doc}
	}
	v := reflect.ValueOf(update)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return update
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		return bson.M{"$set": update}
	}
	return update
}

func hasOperatorKey(doc map[string]any) bool {
	for key := range doc {
		if strings.HasPrefix(key, "$") {
			return true
		}
	}
	return false
}

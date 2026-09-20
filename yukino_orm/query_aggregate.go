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
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (q *Query) Distinct(ctx context.Context, field string) ([]any, error) {
	if err := q.preflight(); err != nil {
		return nil, err
	}
	result := q.collection.Distinct(q.execCtx(ctx), field, q.buildFilter())
	var values []any
	if err := result.Decode(&values); err != nil {
		return nil, err
	}
	return values, nil
}

// CountDistinct returns the number of distinct values of the field among
// matching documents.
func (q *Query) CountDistinct(ctx context.Context, field string) (int64, error) {
	values, err := q.Distinct(ctx, field)
	if err != nil {
		return 0, err
	}
	return int64(len(values)), nil
}

// Pluck collects the value of a single field from all matching documents into
// out, which must be a pointer to a slice of the value type (e.g. *[]string).
// Documents missing the field contribute the zero value. Sort, limit, and
// offset are honored; the Query's projection is not mutated.
func (q *Query) Pluck(ctx context.Context, field string, out any) error {
	if err := q.preflight(); err != nil {
		return err
	}
	if strings.TrimSpace(field) == "" {
		return errors.New("pluck: field is required")
	}
	outVal := reflect.ValueOf(out)
	if outVal.Kind() != reflect.Pointer || outVal.IsNil() || outVal.Elem().Kind() != reflect.Slice {
		return errors.New("pluck: out must be a non-nil pointer to a slice")
	}

	ctx = q.execCtx(ctx)
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
	projection := bson.M{field: 1}
	if field != "_id" {
		projection["_id"] = 0
	}
	opts.SetProjection(projection)

	cursor, err := q.collection.Find(ctx, q.buildFilter(), opts)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	sliceType := outVal.Elem().Type()
	result := reflect.MakeSlice(sliceType, 0, 0)
	elemType := sliceType.Elem()
	path := strings.Split(field, ".")
	for cursor.Next(ctx) {
		elem := reflect.New(elemType)
		raw := cursor.Current.Lookup(path...)
		if raw.Type != 0 {
			if err := raw.Unmarshal(elem.Interface()); err != nil {
				return err
			}
		}
		result = reflect.Append(result, elem.Elem())
	}
	if err := cursor.Err(); err != nil {
		return err
	}
	outVal.Elem().Set(result)
	return nil
}

func (q *Query) Sum(ctx context.Context, field string) (float64, error) {
	return q.aggregate(ctx, bson.M{"$sum": "$" + field})
}

func (q *Query) Avg(ctx context.Context, field string) (float64, error) {
	return q.aggregate(ctx, bson.M{"$avg": "$" + field})
}

func (q *Query) Min(ctx context.Context, field string) (float64, error) {
	return q.aggregate(ctx, bson.M{"$min": "$" + field})
}

func (q *Query) Max(ctx context.Context, field string) (float64, error) {
	return q.aggregate(ctx, bson.M{"$max": "$" + field})
}

func (q *Query) aggregate(ctx context.Context, accumulator bson.M) (float64, error) {
	if err := q.preflight(); err != nil {
		return 0, err
	}
	ctx = q.execCtx(ctx)
	pipeline := bson.A{
		bson.M{"$match": q.buildFilter()},
		bson.M{"$group": bson.M{"_id": nil, "result": accumulator}},
	}
	cursor, err := q.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)
	var results []struct {
		Result float64 `bson:"result"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0].Result, nil
}

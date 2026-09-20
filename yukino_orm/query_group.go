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
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// aggSpec declares one accumulator column of a grouped aggregation.
// An empty field means "count documents" ({$sum: 1}).
type aggSpec struct {
	alias string
	op    string
	field string
}

// GroupBy adds grouping keys for Aggregate. Each key appears in the result
// documents under its own name; dotted paths are flattened with underscores
// (e.g. "addr.city" becomes "addr_city").
func (q *Query) GroupBy(fields ...string) *Query {
	for _, f := range fields {
		if strings.TrimSpace(f) == "" {
			q.setErr(errors.New("groupBy: field must be a non-empty string"))
			return q
		}
	}
	q.groupFields = append(q.groupFields, fields...)
	return q
}

// Having filters grouped rows. It accepts the same argument forms as Where
// and references result column names: group keys or accumulator aliases.
func (q *Query) Having(args ...any) *Query {
	conditions, err := parseWhere(args...)
	if err != nil {
		q.setErr(err)
		return q
	}
	q.havingConds = append(q.havingConds, conditions...)
	return q
}

// CountAs adds a per-group document count under the given alias.
func (q *Query) CountAs(alias string) *Query {
	return q.addAggSpec(alias, "$sum", "")
}

// SumAs adds a per-group sum of field under the given alias.
func (q *Query) SumAs(field string, alias string) *Query {
	return q.addFieldAggSpec(alias, "$sum", field)
}

// AvgAs adds a per-group average of field under the given alias.
func (q *Query) AvgAs(field string, alias string) *Query {
	return q.addFieldAggSpec(alias, "$avg", field)
}

// MinAs adds a per-group minimum of field under the given alias.
func (q *Query) MinAs(field string, alias string) *Query {
	return q.addFieldAggSpec(alias, "$min", field)
}

// MaxAs adds a per-group maximum of field under the given alias.
func (q *Query) MaxAs(field string, alias string) *Query {
	return q.addFieldAggSpec(alias, "$max", field)
}

func (q *Query) addFieldAggSpec(alias string, op string, field string) *Query {
	if strings.TrimSpace(field) == "" {
		q.setErr(fmt.Errorf("%s: field must be a non-empty string", strings.TrimPrefix(op, "$")))
		return q
	}
	return q.addAggSpec(alias, op, field)
}

func (q *Query) addAggSpec(alias string, op string, field string) *Query {
	if strings.TrimSpace(alias) == "" {
		q.setErr(errors.New("aggregate: alias must be a non-empty string"))
		return q
	}
	if alias == "_id" {
		q.setErr(errors.New("aggregate: alias \"_id\" is reserved"))
		return q
	}
	if strings.HasPrefix(alias, "$") || strings.Contains(alias, ".") {
		q.setErr(fmt.Errorf("aggregate: alias %q must not start with \"$\" or contain \".\"", alias))
		return q
	}
	q.aggSpecs = append(q.aggSpecs, aggSpec{alias: alias, op: op, field: field})
	return q
}

// Aggregate executes the grouped aggregation and decodes the result rows into
// out (a pointer to a slice). Each row contains the group keys and the
// accumulator aliases as top-level fields. Where conditions are applied
// before grouping, Having after; OrderBy/Offset/Limit apply to the rows and
// must reference result column names.
func (q *Query) Aggregate(ctx context.Context, out any) error {
	if err := q.preflightBase(); err != nil {
		return err
	}
	pipeline, err := q.buildGroupPipeline()
	if err != nil {
		return err
	}
	ctx = q.execCtx(ctx)
	cursor, err := q.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	return cursor.All(ctx, out)
}

func (q *Query) buildGroupPipeline() (bson.A, error) {
	if len(q.groupFields) == 0 {
		return nil, errors.New("aggregate: GroupBy is required")
	}
	if len(q.fields) > 0 {
		return nil, errors.New("aggregate: Select cannot be combined with GroupBy; result columns are the group keys and aliases")
	}
	keyNames := make(map[string]bool, len(q.groupFields))
	for _, f := range q.groupFields {
		key := groupKeyName(f)
		if keyNames[key] {
			return nil, fmt.Errorf("aggregate: duplicate group key %q (dotted paths are flattened with underscores)", key)
		}
		keyNames[key] = true
	}
	resultCols := make(map[string]bool, len(keyNames)+len(q.aggSpecs))
	for key := range keyNames {
		resultCols[key] = true
	}
	for _, spec := range q.aggSpecs {
		if keyNames[spec.alias] {
			return nil, fmt.Errorf("aggregate: alias %q collides with a group key", spec.alias)
		}
		if resultCols[spec.alias] {
			return nil, fmt.Errorf("aggregate: duplicate alias %q", spec.alias)
		}
		resultCols[spec.alias] = true
	}
	for _, c := range q.havingConds {
		if !resultCols[c.field] {
			return nil, fmt.Errorf("aggregate: having references unknown column %q; available columns: group keys and aliases", c.field)
		}
	}
	for _, e := range q.sort {
		if !resultCols[e.Key] {
			return nil, fmt.Errorf("aggregate: order by references unknown column %q; use the flattened group key or an alias", e.Key)
		}
	}

	pipeline := bson.A{}
	if match := q.buildFilter(); len(match) > 0 {
		pipeline = append(pipeline, bson.M{"$match": match})
	}

	group := bson.M{}
	if len(q.groupFields) == 1 {
		group["_id"] = "$" + q.groupFields[0]
	} else {
		id := bson.M{}
		for _, f := range q.groupFields {
			id[groupKeyName(f)] = "$" + f
		}
		group["_id"] = id
	}
	for _, spec := range q.aggSpecs {
		if spec.field == "" {
			group[spec.alias] = bson.M{"$sum": 1}
			continue
		}
		group[spec.alias] = bson.M{spec.op: "$" + spec.field}
	}
	pipeline = append(pipeline, bson.M{"$group": group})

	project := bson.M{"_id": 0}
	if len(q.groupFields) == 1 {
		project[groupKeyName(q.groupFields[0])] = "$_id"
	} else {
		for _, f := range q.groupFields {
			key := groupKeyName(f)
			project[key] = "$_id." + key
		}
	}
	for _, spec := range q.aggSpecs {
		project[spec.alias] = 1
	}
	pipeline = append(pipeline, bson.M{"$project": project})

	if having := buildConditionFilter(q.havingConds); len(having) > 0 {
		pipeline = append(pipeline, bson.M{"$match": having})
	}
	if len(q.sort) > 0 {
		pipeline = append(pipeline, bson.M{"$sort": q.sort})
	}
	if q.skip > 0 {
		pipeline = append(pipeline, bson.M{"$skip": q.skip})
	}
	if q.limit > 0 {
		pipeline = append(pipeline, bson.M{"$limit": q.limit})
	}
	return pipeline, nil
}

func groupKeyName(field string) string {
	return strings.ReplaceAll(field, ".", "_")
}

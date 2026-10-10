package yukino_orm

import (
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Query struct {
	collection  *mongo.Collection
	engine      *Engine
	conditions  []condition
	orGroups    [][]condition
	sort        bson.D
	limit       int64
	skip        int64
	fields      []string
	groupFields []string
	havingConds []condition
	aggSpecs    []aggSpec
	err         error
}

func (q *Query) setErr(err error) {
	if q.err == nil {
		q.err = err
	}
}

func (q *Query) Where(args ...any) *Query {
	conditions, err := parseWhere(args...)
	if err != nil {
		q.setErr(err)
		return q
	}
	q.conditions = append(q.conditions, conditions...)
	return q
}

func (q *Query) WhereNot(field string, value any) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "$ne", value: value})
	return q
}

func (q *Query) WhereIn(field string, values any) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "$in", value: values})
	return q
}

func (q *Query) WhereNotIn(field string, values any) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "$nin", value: values})
	return q
}

func (q *Query) WhereNull(field string) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "null"})
	return q
}

func (q *Query) WhereNotNull(field string) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "notNull"})
	return q
}

func (q *Query) WhereBetween(field string, low any, high any) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "between", value: [2]any{low, high}})
	return q
}

func (q *Query) WhereNotBetween(field string, low any, high any) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "notBetween", value: [2]any{low, high}})
	return q
}

func (q *Query) WhereLike(field string, pattern string) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "like", value: pattern})
	return q
}

func (q *Query) WhereILike(field string, pattern string) *Query {
	q.conditions = append(q.conditions, condition{field: field, op: "ilike", value: pattern})
	return q
}

func (q *Query) OrWhere(args ...any) *Query {
	conditions, err := parseWhere(args...)
	if err != nil {
		q.setErr(err)
		return q
	}
	if len(conditions) == 0 {
		return q
	}
	q.orGroups = append(q.orGroups, conditions)
	return q
}

func (q *Query) OrWhereNot(field string, value any) *Query {
	q.orGroups = append(q.orGroups, []condition{{field: field, op: "$ne", value: value}})
	return q
}

func (q *Query) OrWhereIn(field string, values any) *Query {
	q.orGroups = append(q.orGroups, []condition{{field: field, op: "$in", value: values}})
	return q
}

func (q *Query) OrWhereNotIn(field string, values any) *Query {
	q.orGroups = append(q.orGroups, []condition{{field: field, op: "$nin", value: values}})
	return q
}

func (q *Query) OrWhereNull(field string) *Query {
	q.orGroups = append(q.orGroups, []condition{{field: field, op: "null"}})
	return q
}

func (q *Query) OrWhereNotNull(field string) *Query {
	q.orGroups = append(q.orGroups, []condition{{field: field, op: "notNull"}})
	return q
}

func (q *Query) OrWhereBetween(field string, low any, high any) *Query {
	q.orGroups = append(q.orGroups, []condition{{field: field, op: "between", value: [2]any{low, high}}})
	return q
}

func (q *Query) OrderBy(field string, direction ...string) *Query {
	dir := 1
	if len(direction) > 0 && strings.EqualFold(strings.TrimSpace(direction[0]), "desc") {
		dir = -1
	}
	q.sort = append(q.sort, bson.E{Key: field, Value: dir})
	return q
}

func (q *Query) Limit(n int64) *Query {
	q.limit = n
	return q
}

func (q *Query) Offset(n int64) *Query {
	q.skip = n
	return q
}

func (q *Query) Select(fields ...string) *Query {
	q.fields = fields
	return q
}

func (q *Query) Clone() *Query {
	if q == nil {
		return nil
	}
	clone := &Query{
		collection: q.collection,
		engine:     q.engine,
		limit:      q.limit,
		skip:       q.skip,
		err:        q.err,
	}
	clone.conditions = append([]condition(nil), q.conditions...)
	if q.orGroups != nil {
		clone.orGroups = make([][]condition, len(q.orGroups))
		for i, group := range q.orGroups {
			clone.orGroups[i] = append([]condition(nil), group...)
		}
	}
	clone.sort = append(bson.D(nil), q.sort...)
	clone.fields = append([]string(nil), q.fields...)
	clone.groupFields = append([]string(nil), q.groupFields...)
	clone.havingConds = append([]condition(nil), q.havingConds...)
	clone.aggSpecs = append([]aggSpec(nil), q.aggSpecs...)
	return clone
}

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
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const defaultTestMongoURI = "mongodb://localhost:27017/"

type testUser struct {
	ID        int64     `bson:"_id"`
	Name      string    `bson:"name"`
	Email     string    `bson:"email"`
	Age       int       `bson:"age"`
	CreatedAt time.Time `bson:"created_at"`
}

type testCity struct {
	Name string `bson:"name"`
}

func openTestEngine(t *testing.T) (*Engine, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = defaultTestMongoURI
	}
	database := fmt.Sprintf("yukino_orm_test_%d", time.Now().UnixNano())
	engine, err := NewEngine(ctx, uri, database)
	if err != nil {
		t.Fatalf("NewEngine returned error: %v", err)
	}
	if err := engine.Database().RunCommand(ctx, bson.D{{Key: "create", Value: "__access_check"}}).Err(); err != nil {
		_ = engine.Close(ctx)
		if isMongoUnauthorized(err) {
			t.Skipf("MongoDB requires authentication; set MONGO_URI with credentials: %v", err)
		}
		t.Fatalf("MongoDB access check returned error: %v", err)
	}
	_ = engine.Collection("__access_check").DropCollection(ctx)
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()
		if err := engine.DropDatabase(dropCtx); err != nil {
			if isMongoUnauthorized(err) {
				return
			}
			t.Fatalf("DropDatabase returned error: %v", err)
		}
		_ = engine.Close(dropCtx)
	})
	return engine, ctx
}

func isMongoUnauthorized(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Unauthorized")
}

func TestNewEngineValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := NewEngine(ctx, "", "db"); err == nil {
		t.Fatal("expected error for empty uri")
	}
	if _, err := NewEngine(ctx, "mongodb://localhost:27017", ""); err == nil {
		t.Fatal("expected error for empty database")
	}
	var e *Engine
	if err := e.Close(ctx); err != nil {
		t.Fatalf("nil Close returned error: %v", err)
	}
	if e.Client() != nil || e.Database() != nil || e.DatabaseName() != "" {
		t.Fatal("nil engine accessors should return zero values")
	}
}

func TestCollectionName(t *testing.T) {
	cases := map[string]any{
		"test_users":  &testUser{},
		"test_cities": []testCity{},
		"":            42,
	}
	for want, input := range cases {
		if got := CollectionName(input); got != want {
			t.Fatalf("CollectionName(%T) = %q, want %q", input, got, want)
		}
	}
}

func TestWhereInsertFindUpdateDeleteCount(t *testing.T) {
	engine, ctx := openTestEngine(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	_, err := engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom", Email: "tom@example.com", Age: 18, CreatedAt: now},
		&testUser{ID: 2, Name: "Sam", Email: "sam@example.com", Age: 20, CreatedAt: now},
		&testUser{ID: 3, Name: "Amy", Email: "amy@example.com", Age: 25, CreatedAt: now},
	)
	if err != nil {
		t.Fatalf("Insert returned error: %v", err)
	}

	count, err := engine.Collection("users").Count(ctx)
	if err != nil || count != 3 {
		t.Fatalf("Count = %d, %v", count, err)
	}

	var first testUser
	err = engine.Collection("users").
		Where("age", ">=", 18).
		OrderBy("age", "desc").
		First(ctx, &first)
	if err != nil || first.Name != "Amy" {
		t.Fatalf("First = %+v, %v", first, err)
	}

	var users []testUser
	err = engine.Collection("users").
		Where("age", ">=", 18).
		OrderBy("age").
		Limit(2).
		Offset(1).
		Find(ctx, &users)
	if err != nil || len(users) != 2 || users[0].Name != "Sam" {
		t.Fatalf("Find = %+v, %v", users, err)
	}

	modified, err := engine.Collection("users").
		Where("name", "Tom").
		Update(ctx, bson.M{"age": 19})
	if err != nil || modified != 1 {
		t.Fatalf("Update = %d, %v", modified, err)
	}

	deleted, err := engine.Collection("users").
		Where("age", ">=", 19).
		Delete(ctx)
	if err != nil || deleted != 3 {
		t.Fatalf("Delete = %d, %v", deleted, err)
	}
}

func TestWhereInAndBetween(t *testing.T) {
	engine, ctx := openTestEngine(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom", Age: 18, CreatedAt: now},
		&testUser{ID: 2, Name: "Sam", Age: 25, CreatedAt: now},
		&testUser{ID: 3, Name: "Amy", Age: 30, CreatedAt: now},
	)

	var users []testUser
	err := engine.Collection("users").
		WhereIn("name", []string{"Tom", "Amy"}).
		OrderBy("age").
		Find(ctx, &users)
	if err != nil || len(users) != 2 || users[0].Name != "Tom" || users[1].Name != "Amy" {
		t.Fatalf("WhereIn = %+v, %v", users, err)
	}

	users = nil
	err = engine.Collection("users").
		WhereBetween("age", 20, 30).
		Find(ctx, &users)
	if err != nil || len(users) != 2 {
		t.Fatalf("WhereBetween = %+v, %v", users, err)
	}
}

func TestOrWhere(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom", Age: 18},
		&testUser{ID: 2, Name: "Sam", Age: 25},
		&testUser{ID: 3, Name: "Amy", Age: 30},
	)

	var users []testUser
	err := engine.Collection("users").
		Where("name", "Tom").
		OrWhere("name", "Amy").
		OrderBy("age").
		Find(ctx, &users)
	if err != nil || len(users) != 2 {
		t.Fatalf("OrWhere = %+v, %v", users, err)
	}
}

func TestSelectProjection(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx, &testUser{ID: 1, Name: "Tom", Email: "tom@e.com", Age: 18})

	var users []testUser
	err := engine.Collection("users").
		Select("name", "_id").
		Find(ctx, &users)
	if err != nil || len(users) != 1 || users[0].Name != "Tom" || users[0].Email != "" {
		t.Fatalf("Select = %+v, %v", users, err)
	}
}

func TestExists(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx, &testUser{ID: 1, Name: "Tom"})

	exists, err := engine.Collection("users").Where("name", "Tom").Exists(ctx)
	if err != nil || !exists {
		t.Fatalf("Exists(Tom) = %v, %v", exists, err)
	}
	exists, err = engine.Collection("users").Where("name", "Nobody").Exists(ctx)
	if err != nil || exists {
		t.Fatalf("Exists(Nobody) = %v, %v", exists, err)
	}
}

func TestAggregation(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom", Age: 10},
		&testUser{ID: 2, Name: "Sam", Age: 20},
		&testUser{ID: 3, Name: "Amy", Age: 30},
	)

	sum, err := engine.Collection("users").Sum(ctx, "age")
	if err != nil || sum != 60 {
		t.Fatalf("Sum = %v, %v", sum, err)
	}
	avg, err := engine.Collection("users").Avg(ctx, "age")
	if err != nil || avg != 20 {
		t.Fatalf("Avg = %v, %v", avg, err)
	}
	min, err := engine.Collection("users").Min(ctx, "age")
	if err != nil || min != 10 {
		t.Fatalf("Min = %v, %v", min, err)
	}
	max, err := engine.Collection("users").Max(ctx, "age")
	if err != nil || max != 30 {
		t.Fatalf("Max = %v, %v", max, err)
	}
}

func TestDistinct(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom", Age: 18},
		&testUser{ID: 2, Name: "Sam", Age: 18},
		&testUser{ID: 3, Name: "Amy", Age: 25},
	)

	values, err := engine.Collection("users").Distinct(ctx, "age")
	if err != nil || len(values) != 2 {
		t.Fatalf("Distinct = %v, %v", values, err)
	}
}

func TestEnsureIndexesAndDrop(t *testing.T) {
	engine, ctx := openTestEngine(t)
	names, err := engine.Collection("users").EnsureIndexes(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_email")},
	})
	if err != nil || len(names) != 1 || names[0] != "uniq_email" {
		t.Fatalf("EnsureIndexes = %v, %v", names, err)
	}
	engine.Collection("users").Insert(ctx, &testUser{ID: 1, Email: "dupe@e.com"})
	if _, err := engine.Collection("users").Insert(ctx, &testUser{ID: 2, Email: "dupe@e.com"}); err == nil {
		t.Fatal("expected unique index error")
	}
	if err := engine.Collection("users").DropCollection(ctx); err != nil {
		t.Fatalf("DropCollection returned error: %v", err)
	}
	if err := engine.Collection("users").First(ctx, &testUser{}); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("First after drop = %v", err)
	}
}

func TestNextSequence(t *testing.T) {
	engine, ctx := openTestEngine(t)
	first, err := engine.NextSequence(ctx, "users")
	if err != nil || first != 1 {
		t.Fatalf("first = %d, %v", first, err)
	}
	second, err := engine.NextSequence(ctx, "users")
	if err != nil || second != 2 {
		t.Fatalf("second = %d, %v", second, err)
	}
	if _, err := engine.NextSequence(ctx, ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestQueryValidation(t *testing.T) {
	engine, ctx := openTestEngine(t)
	q := engine.Collection("")
	if _, err := q.Insert(ctx); err == nil {
		t.Fatal("expected collection required error")
	}
	if _, err := engine.Collection("test").Insert(ctx); err == nil {
		t.Fatal("expected empty insert error")
	}
}

func TestModelEntryPoint(t *testing.T) {
	engine, ctx := openTestEngine(t)
	_, err := engine.Model(&testUser{}).Insert(ctx, &testUser{ID: 1, Name: "model"})
	if err != nil {
		t.Fatalf("Model insert returned error: %v", err)
	}
	var out testUser
	err = engine.Model(&testUser{}).Where("name", "model").First(ctx, &out)
	if err != nil || out.Name != "model" {
		t.Fatalf("Model first = %+v, %v", out, err)
	}
}

func TestBuildFilterMergesMultipleOpsOnSameField(t *testing.T) {
	q := &Query{}
	q.Where("age", ">", 18).Where("age", "<", 30)
	filter := q.buildFilter()
	ageFilter, ok := filter["age"].(bson.M)
	if !ok {
		t.Fatalf("age filter is not bson.M: %#v", filter["age"])
	}
	if ageFilter["$gt"] != 18 || ageFilter["$lt"] != 30 {
		t.Fatalf("merged filter = %#v, want $gt:18 $lt:30", ageFilter)
	}
}

func TestBuildFilterWhereNull(t *testing.T) {
	q := &Query{}
	q.WhereNull("deleted_at").Where("active", true)
	filter := q.buildFilter()
	if filter["deleted_at"] != nil {
		t.Fatalf("WhereNull should set field to nil, got %#v", filter["deleted_at"])
	}
	if _, exists := filter["deleted_at"]; !exists {
		t.Fatal("WhereNull field should exist in filter with nil value")
	}
	if filter["active"] != true {
		t.Fatalf("other conditions lost: %#v", filter)
	}
}

func TestBuildFilterWhereBetween(t *testing.T) {
	q := &Query{}
	q.WhereBetween("age", 18, 30)
	filter := q.buildFilter()
	ageFilter, ok := filter["age"].(bson.M)
	if !ok {
		t.Fatalf("age filter is not bson.M: %#v", filter["age"])
	}
	if ageFilter["$gte"] != 18 || ageFilter["$lte"] != 30 {
		t.Fatalf("between filter = %#v", ageFilter)
	}
}

func TestPluralize(t *testing.T) {
	cases := map[string]string{
		"user":     "users",
		"city":     "cities",
		"category": "categories",
		"day":      "days",
		"key":      "keys",
		"boy":      "boys",
		"bus":      "buses",
		"address":  "addresses",
		"fox":      "foxes",
		"match":    "matches",
		"dish":     "dishes",
	}
	for input, want := range cases {
		if got := pluralize(input); got != want {
			t.Fatalf("pluralize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeUpdate(t *testing.T) {
	plain := normalizeUpdate(bson.M{"name": "Tom", "age": 20})
	if set, ok := plain.(bson.M)["$set"]; !ok {
		t.Fatalf("plain update not wrapped: %#v", plain)
	} else {
		inner := set.(bson.M)
		if inner["name"] != "Tom" {
			t.Fatalf("$set content = %#v", inner)
		}
	}

	withOp := normalizeUpdate(bson.M{"$inc": bson.M{"count": 1}})
	if _, ok := withOp.(bson.M)["$inc"]; !ok {
		t.Fatalf("operator update should pass through: %#v", withOp)
	}
}

func TestBuildFilterEqualityThenOperatorPreserved(t *testing.T) {
	q := &Query{}
	q.Where("age", 18).Where("age", ">", 10)
	filter := q.buildFilter()
	if filter["age"] != 18 {
		t.Fatalf("equality lost: %#v", filter)
	}
	and, ok := filter["$and"].([]bson.M)
	if !ok || len(and) != 1 {
		t.Fatalf("operator not preserved via $and: %#v", filter)
	}
	if got := and[0]["age"].(bson.M)["$gt"]; got != 10 {
		t.Fatalf("$gt clause = %#v", and[0])
	}
}

func TestBuildFilterOperatorThenEqualityPreserved(t *testing.T) {
	q := &Query{}
	q.Where("age", ">", 10).Where("age", 18)
	filter := q.buildFilter()
	ageFilter, ok := filter["age"].(bson.M)
	if !ok || ageFilter["$gt"] != 10 || ageFilter["$eq"] != 18 {
		t.Fatalf("conditions lost: %#v", filter)
	}
}

func TestBuildFilterDuplicateEqualityPreserved(t *testing.T) {
	q := &Query{}
	q.Where("a", 1).Where("a", 2)
	filter := q.buildFilter()
	if filter["a"] != 1 {
		t.Fatalf("first equality lost: %#v", filter)
	}
	and, ok := filter["$and"].([]bson.M)
	if !ok || len(and) != 1 || and[0]["a"] != 2 {
		t.Fatalf("second equality lost: %#v", filter)
	}
}

func TestBuildFilterOrWhereWithoutBaseCondition(t *testing.T) {
	q := &Query{}
	q.OrWhere("name", "Tom").OrWhere("name", "Amy")
	filter := q.buildFilter()
	or, ok := filter["$or"].([]bson.M)
	if !ok || len(or) != 2 {
		t.Fatalf("expected 2 or-branches without empty base, got %#v", filter)
	}
	for _, clause := range or {
		if len(clause) == 0 {
			t.Fatalf("empty clause degenerates to match-all: %#v", filter)
		}
	}
}

func TestBuildFilterSingleOrWhereActsAsWhere(t *testing.T) {
	q := &Query{}
	q.OrWhere("name", "Tom")
	filter := q.buildFilter()
	if _, hasOr := filter["$or"]; hasOr {
		t.Fatalf("single or-branch should not be wrapped in $or: %#v", filter)
	}
	if filter["name"] != "Tom" {
		t.Fatalf("filter = %#v", filter)
	}
}

func TestWhereObjectForm(t *testing.T) {
	q := &Query{}
	q.Where(bson.M{"b": 2, "a": 1, "c": nil})
	filter := q.buildFilter()
	if filter["a"] != 1 || filter["b"] != 2 {
		t.Fatalf("object form = %#v", filter)
	}
	if v, exists := filter["c"]; !exists || v != nil {
		t.Fatalf("nil map value should become null check: %#v", filter)
	}
}

func TestWhereInvalidArgsReturnErrorNotPanic(t *testing.T) {
	engine := &Engine{}
	cases := []*Query{
		(&Query{}).Where(123, "x"),
		(&Query{}).Where("f", 1, 2),
		(&Query{}).Where("f", "like", 42),
		(&Query{}).Where("f", "bogus", 1),
		(&Query{}).Where(),
		(&Query{}).Where("f", "between", 5),
	}
	_ = engine
	for i, q := range cases {
		if q.err == nil {
			t.Fatalf("case %d: expected builder error", i)
		}
		if err := q.preflight(); err == nil || errors.Is(err, ErrCollectionRequired) {
			t.Fatalf("case %d: builder error should surface first, got %v", i, err)
		}
	}
}

func TestWhereOperatorAliases(t *testing.T) {
	q := &Query{}
	q.Where("a", "in", []int{1, 2}).Where("b", "not in", []int{3}).Where("c", "between", []int{1, 9})
	if q.err != nil {
		t.Fatalf("aliases rejected: %v", q.err)
	}
	filter := q.buildFilter()
	if _, ok := filter["a"].(bson.M)["$in"]; !ok {
		t.Fatalf("in alias = %#v", filter)
	}
	if _, ok := filter["b"].(bson.M)["$nin"]; !ok {
		t.Fatalf("not in alias = %#v", filter)
	}
	c := filter["c"].(bson.M)
	if c["$gte"] != 1 || c["$lte"] != 9 {
		t.Fatalf("between alias = %#v", filter)
	}
}

func TestLikeToRegex(t *testing.T) {
	cases := map[string]string{
		"tom":   "^tom$",
		"%tom%": "^.*tom.*$",
		"to_m":  "^to.m$",
		"a.b%":  "^a\\.b.*$",
		"100%":  "^100.*$",
	}
	for input, want := range cases {
		if got := likeToRegex(input); got != want {
			t.Fatalf("likeToRegex(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWhereNotBetweenFilter(t *testing.T) {
	q := &Query{}
	q.WhereNotBetween("age", 18, 30)
	filter := q.buildFilter()
	not, ok := filter["age"].(bson.M)["$not"].(bson.M)
	if !ok || not["$gte"] != 18 || not["$lte"] != 30 {
		t.Fatalf("not between = %#v", filter)
	}
}

func TestNormalizeUpdateExtended(t *testing.T) {
	if m, ok := normalizeUpdate(map[string]any{"name": "Tom"}).(bson.M); !ok || m["$set"] == nil {
		t.Fatalf("plain map not wrapped")
	}
	if m, ok := normalizeUpdate(bson.D{{Key: "name", Value: "Tom"}}).(bson.M); !ok || m["$set"] == nil {
		t.Fatalf("bson.D not wrapped")
	}
	if d, ok := normalizeUpdate(bson.D{{Key: "$inc", Value: 1}}).(bson.D); !ok || d[0].Key != "$inc" {
		t.Fatalf("bson.D with operator should pass through")
	}
	type patch struct {
		Name string `bson:"name"`
	}
	if m, ok := normalizeUpdate(patch{Name: "Tom"}).(bson.M); !ok || m["$set"] == nil {
		t.Fatalf("struct not wrapped")
	}
	if m, ok := normalizeUpdate(&patch{Name: "Tom"}).(bson.M); !ok || m["$set"] == nil {
		t.Fatalf("struct pointer not wrapped")
	}
}

func TestExpandInsertDocs(t *testing.T) {
	users := []*testUser{{ID: 1}, {ID: 2}}
	if got := expandInsertDocs([]any{users}); len(got) != 2 {
		t.Fatalf("pointer slice not expanded: %d", len(got))
	}
	values := []testUser{{ID: 1}, {ID: 2}, {ID: 3}}
	if got := expandInsertDocs([]any{values}); len(got) != 3 {
		t.Fatalf("value slice not expanded: %d", len(got))
	}
	doc := bson.D{{Key: "name", Value: "Tom"}}
	if got := expandInsertDocs([]any{doc}); len(got) != 1 {
		t.Fatalf("bson.D must stay a single document: %d", len(got))
	}
	raw := []byte{1, 2, 3}
	if got := expandInsertDocs([]any{raw}); len(got) != 1 {
		t.Fatalf("byte slice must stay a single argument: %d", len(got))
	}
	if got := expandInsertDocs([]any{&testUser{ID: 1}, &testUser{ID: 2}}); len(got) != 2 {
		t.Fatalf("multi-arg form must pass through: %d", len(got))
	}
}

func TestCloneIndependence(t *testing.T) {
	base := &Query{}
	base.Where("a", 1).OrWhere("b", 2).OrderBy("a").Select("a")
	clone := base.Clone()
	clone.Where("c", 3).OrWhere("d", 4).OrderBy("c").Select("c")

	baseFilter := base.buildFilter()
	or := baseFilter["$or"].([]bson.M)
	if len(or) != 2 {
		t.Fatalf("base or-branches polluted by clone: %#v", baseFilter)
	}
	if _, exists := or[0]["c"]; exists {
		t.Fatalf("base conditions polluted by clone: %#v", baseFilter)
	}
	if len(base.sort) != 1 || len(base.fields) != 1 || base.fields[0] != "a" {
		t.Fatalf("base sort/fields polluted: %#v %#v", base.sort, base.fields)
	}
	cloneFilter := clone.buildFilter()
	if len(cloneFilter["$or"].([]bson.M)) != 3 {
		t.Fatalf("clone missing own branch: %#v", cloneFilter)
	}
}

func TestBuildProjectionExclusion(t *testing.T) {
	q := &Query{}
	q.Select("name", "-_id")
	proj := q.buildProjection()
	if proj["name"] != 1 || proj["_id"] != 0 {
		t.Fatalf("projection = %#v", proj)
	}
}

func TestOrderByCaseInsensitive(t *testing.T) {
	q := &Query{}
	q.OrderBy("a", "Desc").OrderBy("b", " DESC ").OrderBy("c", "asc").OrderBy("d")
	want := []int{-1, -1, 1, 1}
	for i, e := range q.sort {
		if e.Value != want[i] {
			t.Fatalf("sort[%d] = %v, want %d", i, e.Value, want[i])
		}
	}
}

func TestPluckStringsAndInts(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom", Age: 18},
		&testUser{ID: 2, Name: "Sam", Age: 25},
		&testUser{ID: 3, Name: "Amy", Age: 30},
	)

	var names []string
	err := engine.Collection("users").Where("age", ">=", 25).OrderBy("age").Pluck(ctx, "name", &names)
	if err != nil || len(names) != 2 || names[0] != "Sam" || names[1] != "Amy" {
		t.Fatalf("Pluck names = %v, %v", names, err)
	}

	var ages []int64
	err = engine.Collection("users").OrderBy("age", "desc").Limit(2).Pluck(ctx, "age", &ages)
	if err != nil || len(ages) != 2 || ages[0] != 30 || ages[1] != 25 {
		t.Fatalf("Pluck ages = %v, %v", ages, err)
	}

	var missing []string
	err = engine.Collection("users").Pluck(ctx, "not_a_field", &missing)
	if err != nil || len(missing) != 3 || missing[0] != "" {
		t.Fatalf("Pluck missing field = %v, %v", missing, err)
	}

	q := engine.Collection("users")
	if err := q.Pluck(ctx, "name", &names); err != nil {
		t.Fatalf("Pluck returned error: %v", err)
	}
	var all []testUser
	if err := q.Find(ctx, &all); err != nil || len(all) != 3 || all[0].Age == 0 {
		t.Fatalf("Pluck must not mutate projection: %+v, %v", all, err)
	}
}

func TestOrWhereOnlyDoesNotMatchAll(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom"},
		&testUser{ID: 2, Name: "Sam"},
		&testUser{ID: 3, Name: "Amy"},
	)
	count, err := engine.Collection("users").OrWhere("name", "Tom").OrWhere("name", "Amy").Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("OrWhere-only count = %d, %v (want 2, not full collection)", count, err)
	}
	deleted, err := engine.Collection("users").OrWhere("name", "Nobody").Delete(ctx)
	if err != nil || deleted != 0 {
		t.Fatalf("OrWhere-only delete = %d, %v (must not wipe collection)", deleted, err)
	}
}

func TestSameFieldEqualityAndRangeIntegration(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Age: 15},
		&testUser{ID: 2, Age: 20},
		&testUser{ID: 3, Age: 35},
	)
	count, err := engine.Collection("users").Where("age", 20).Where("age", ">", 18).Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("equality+range = %d, %v (want 1)", count, err)
	}
	count, err = engine.Collection("users").Where("age", ">", 18).Where("age", 15).Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("range+equality = %d, %v (want 0)", count, err)
	}
}

func TestUpdateReturnsMatchedCount(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx, &testUser{ID: 1, Name: "Tom", Age: 18})
	matched, err := engine.Collection("users").Where("_id", int64(1)).Update(ctx, bson.M{"age": 18})
	if err != nil || matched != 1 {
		t.Fatalf("idempotent update matched = %d, %v (want 1)", matched, err)
	}
}

func TestIncrementDecrement(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx, &testUser{ID: 1, Name: "Tom", Age: 18})
	if n, err := engine.Collection("users").Where("_id", int64(1)).Increment(ctx, "age"); err != nil || n != 1 {
		t.Fatalf("Increment = %d, %v", n, err)
	}
	if n, err := engine.Collection("users").Where("_id", int64(1)).Increment(ctx, "age", 5); err != nil || n != 1 {
		t.Fatalf("Increment 5 = %d, %v", n, err)
	}
	if n, err := engine.Collection("users").Where("_id", int64(1)).Decrement(ctx, "age", 4); err != nil || n != 1 {
		t.Fatalf("Decrement 4 = %d, %v", n, err)
	}
	var u testUser
	if err := engine.Collection("users").Where("_id", int64(1)).First(ctx, &u); err != nil || u.Age != 20 {
		t.Fatalf("age after inc/dec = %d, %v (want 20)", u.Age, err)
	}
}

func TestUpsert(t *testing.T) {
	engine, ctx := openTestEngine(t)
	res, err := engine.Collection("users").Where("_id", int64(1)).Upsert(ctx, bson.M{"name": "Tom", "age": 18})
	if err != nil || res.UpsertedCount != 1 {
		t.Fatalf("insert-path upsert = %+v, %v", res, err)
	}
	res, err = engine.Collection("users").Where("_id", int64(1)).Upsert(ctx, bson.M{"name": "Tommy"})
	if err != nil || res.MatchedCount != 1 || res.UpsertedCount != 0 {
		t.Fatalf("update-path upsert = %+v, %v", res, err)
	}
	var u testUser
	if err := engine.Collection("users").Where("_id", int64(1)).First(ctx, &u); err != nil || u.Name != "Tommy" || u.Age != 18 {
		t.Fatalf("after upsert = %+v, %v", u, err)
	}
}

func TestInsertSliceArgument(t *testing.T) {
	engine, ctx := openTestEngine(t)
	users := []*testUser{{ID: 1, Name: "Tom"}, {ID: 2, Name: "Sam"}}
	res, err := engine.Collection("users").Insert(ctx, users)
	if err != nil || res.InsertedCount != 2 {
		t.Fatalf("Insert(slice) = %+v, %v", res, err)
	}
	values := []testUser{{ID: 3, Name: "Amy"}}
	if res, err := engine.Collection("users").Insert(ctx, values); err != nil || res.InsertedCount != 1 {
		t.Fatalf("Insert(value slice) = %+v, %v", res, err)
	}
}

func TestWhereLikeIntegration(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom"},
		&testUser{ID: 2, Name: "Tommy"},
		&testUser{ID: 3, Name: "Sam"},
	)
	count, err := engine.Collection("users").WhereLike("name", "Tom%").Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("WhereLike Tom%% = %d, %v", count, err)
	}
	count, err = engine.Collection("users").WhereLike("name", "tom%").Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("WhereLike is case-sensitive, got %d, %v", count, err)
	}
	count, err = engine.Collection("users").WhereILike("name", "tom%").Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("WhereILike tom%% = %d, %v", count, err)
	}
	count, err = engine.Collection("users").WhereLike("name", "T_m").Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("WhereLike T_m = %d, %v", count, err)
	}
}

func TestWhereNotAndOrVariants(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Name: "Tom", Age: 18},
		&testUser{ID: 2, Name: "Sam", Age: 25},
		&testUser{ID: 3, Name: "Amy", Age: 30},
	)
	count, err := engine.Collection("users").WhereNot("name", "Tom").Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("WhereNot = %d, %v", count, err)
	}
	count, err = engine.Collection("users").Where("name", "Tom").OrWhereIn("age", []int{25, 30}).Count(ctx)
	if err != nil || count != 3 {
		t.Fatalf("OrWhereIn = %d, %v", count, err)
	}
	count, err = engine.Collection("users").Where("name", "Tom").OrWhereBetween("age", 26, 40).Count(ctx)
	if err != nil || count != 2 {
		t.Fatalf("OrWhereBetween = %d, %v", count, err)
	}
	count, err = engine.Collection("users").WhereNotBetween("age", 20, 40).Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("WhereNotBetween = %d, %v", count, err)
	}
}

func TestCountDistinct(t *testing.T) {
	engine, ctx := openTestEngine(t)
	engine.Collection("users").Insert(ctx,
		&testUser{ID: 1, Age: 18},
		&testUser{ID: 2, Age: 18},
		&testUser{ID: 3, Age: 25},
	)
	n, err := engine.Collection("users").CountDistinct(ctx, "age")
	if err != nil || n != 2 {
		t.Fatalf("CountDistinct = %d, %v", n, err)
	}
}

func TestTransactionAutoSessionBinding(t *testing.T) {
	engine, ctx := openTestEngine(t)
	if _, err := engine.Collection("users").Insert(ctx, &testUser{ID: 1, Name: "Tom", Age: 18}); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	sentinel := errors.New("rollback")
	err := engine.Transaction(ctx, func(sc context.Context, tx *Engine) error {
		// Intentionally pass the plain outer ctx: execCtx must bind the session.
		if _, err := tx.Collection("users").Where("_id", int64(1)).Update(ctx, bson.M{"age": 99}); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("expected transaction error")
	}
	if !errors.Is(err, sentinel) {
		if strings.Contains(err.Error(), "Transaction numbers") || strings.Contains(err.Error(), "IllegalOperation") {
			t.Skipf("MongoDB deployment does not support transactions: %v", err)
		}
		t.Fatalf("Transaction returned unexpected error: %v", err)
	}
	var u testUser
	if err := engine.Collection("users").Where("_id", int64(1)).First(ctx, &u); err != nil || u.Age != 18 {
		t.Fatalf("rollback failed, age = %d, %v (want 18)", u.Age, err)
	}
}

func TestFirstErrNotFoundAlias(t *testing.T) {
	engine, ctx := openTestEngine(t)
	err := engine.Collection("users").Where("name", "Nobody").First(ctx, &testUser{})
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, mongo.ErrNoDocuments) {
		t.Fatalf("First no-match = %v", err)
	}
}

func TestGroupPipelineShape(t *testing.T) {
	q := &Query{}
	q.Where("status", "paid").
		GroupBy("city").
		CountAs("n").
		SumAs("amount", "total").
		Having("n", ">", 1).
		OrderBy("total", "desc").
		Offset(1).
		Limit(2)
	pipeline, err := q.buildGroupPipeline()
	if err != nil {
		t.Fatalf("buildGroupPipeline: %v", err)
	}
	if len(pipeline) != 7 {
		t.Fatalf("stage count = %d, want 7: %#v", len(pipeline), pipeline)
	}
	if m := pipeline[0].(bson.M)["$match"].(bson.M); m["status"] != "paid" {
		t.Fatalf("where stage = %#v", pipeline[0])
	}
	group := pipeline[1].(bson.M)["$group"].(bson.M)
	if group["_id"] != "$city" {
		t.Fatalf("group _id = %#v", group)
	}
	if n := group["n"].(bson.M); n["$sum"] != 1 {
		t.Fatalf("count accumulator = %#v", group)
	}
	if total := group["total"].(bson.M); total["$sum"] != "$amount" {
		t.Fatalf("sum accumulator = %#v", group)
	}
	project := pipeline[2].(bson.M)["$project"].(bson.M)
	if project["_id"] != 0 || project["city"] != "$_id" || project["n"] != 1 || project["total"] != 1 {
		t.Fatalf("project stage = %#v", project)
	}
	having := pipeline[3].(bson.M)["$match"].(bson.M)
	if having["n"].(bson.M)["$gt"] != 1 {
		t.Fatalf("having stage = %#v", having)
	}
	sortStage, ok := pipeline[4].(bson.M)["$sort"].(bson.D)
	if !ok || len(sortStage) != 1 || sortStage[0].Key != "total" || sortStage[0].Value != -1 {
		t.Fatalf("sort stage = %#v", pipeline[4])
	}
	if pipeline[5].(bson.M)["$skip"] != int64(1) || pipeline[6].(bson.M)["$limit"] != int64(2) {
		t.Fatalf("skip/limit stages = %#v %#v", pipeline[5], pipeline[6])
	}
}

func TestGroupPipelineMultiFieldAndDotted(t *testing.T) {
	q := &Query{}
	q.GroupBy("addr.city", "status")
	pipeline, err := q.buildGroupPipeline()
	if err != nil {
		t.Fatalf("buildGroupPipeline: %v", err)
	}
	id := pipeline[0].(bson.M)["$group"].(bson.M)["_id"].(bson.M)
	if id["addr_city"] != "$addr.city" || id["status"] != "$status" {
		t.Fatalf("group _id = %#v", id)
	}
	project := pipeline[1].(bson.M)["$project"].(bson.M)
	if project["addr_city"] != "$_id.addr_city" || project["status"] != "$_id.status" {
		t.Fatalf("project stage = %#v", project)
	}
}

func TestGroupBuilderValidation(t *testing.T) {
	if q := (&Query{}).GroupBy(""); q.err == nil {
		t.Fatal("empty group field must error")
	}
	if q := (&Query{}).GroupBy("city").CountAs(""); q.err == nil {
		t.Fatal("empty alias must error")
	}
	if q := (&Query{}).GroupBy("city").SumAs("", "total"); q.err == nil {
		t.Fatal("empty agg field must error")
	}
	if q := (&Query{}).GroupBy("city").CountAs("_id"); q.err == nil {
		t.Fatal("_id alias must error")
	}
	if q := (&Query{}).GroupBy("city").Having("n", "bogus", 1); q.err == nil {
		t.Fatal("invalid having operator must error")
	}
	if _, err := (&Query{}).buildGroupPipeline(); err == nil {
		t.Fatal("Aggregate without GroupBy must error")
	}
	if _, err := (&Query{}).GroupBy("city").CountAs("city").buildGroupPipeline(); err == nil {
		t.Fatal("alias colliding with group key must error")
	}
	if _, err := (&Query{}).GroupBy("city").CountAs("n").SumAs("amount", "n").buildGroupPipeline(); err == nil {
		t.Fatal("duplicate alias must error")
	}
}

func TestCloneCopiesGroupState(t *testing.T) {
	base := (&Query{}).GroupBy("city").CountAs("n").Having("n", ">", 1)
	clone := base.Clone()
	clone.GroupBy("status").SumAs("amount", "total").Having("total", ">", 10)
	if len(base.groupFields) != 1 || len(base.aggSpecs) != 1 || len(base.havingConds) != 1 {
		t.Fatalf("base group state polluted: %#v %#v %#v", base.groupFields, base.aggSpecs, base.havingConds)
	}
	if len(clone.groupFields) != 2 || len(clone.aggSpecs) != 2 || len(clone.havingConds) != 2 {
		t.Fatalf("clone group state wrong: %#v", clone)
	}
}

type testOrder struct {
	ID     int64   `bson:"_id"`
	City   string  `bson:"city"`
	Status string  `bson:"status"`
	Amount float64 `bson:"amount"`
}

func seedOrders(t *testing.T, engine *Engine, ctx context.Context) {
	t.Helper()
	_, err := engine.Collection("orders").Insert(ctx, []*testOrder{
		{ID: 1, City: "SH", Status: "paid", Amount: 10},
		{ID: 2, City: "SH", Status: "paid", Amount: 20},
		{ID: 3, City: "BJ", Status: "paid", Amount: 5},
		{ID: 4, City: "BJ", Status: "unpaid", Amount: 50},
		{ID: 5, City: "HZ", Status: "paid", Amount: 7},
	})
	if err != nil {
		t.Fatalf("seed orders: %v", err)
	}
}

func TestGroupByHavingAggregate(t *testing.T) {
	engine, ctx := openTestEngine(t)
	seedOrders(t, engine, ctx)

	type cityAgg struct {
		City  string  `bson:"city"`
		N     int64   `bson:"n"`
		Total float64 `bson:"total"`
	}
	var rows []cityAgg
	err := engine.Collection("orders").
		Where("status", "paid").
		GroupBy("city").
		CountAs("n").
		SumAs("amount", "total").
		OrderBy("total", "desc").
		Aggregate(ctx, &rows)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(rows) != 3 || rows[0].City != "SH" || rows[0].N != 2 || rows[0].Total != 30 {
		t.Fatalf("Aggregate rows = %+v", rows)
	}

	rows = nil
	err = engine.Collection("orders").
		Where("status", "paid").
		GroupBy("city").
		CountAs("n").
		SumAs("amount", "total").
		Having("n", ">=", 2).
		Aggregate(ctx, &rows)
	if err != nil || len(rows) != 1 || rows[0].City != "SH" {
		t.Fatalf("Having rows = %+v, %v", rows, err)
	}

	rows = nil
	err = engine.Collection("orders").
		GroupBy("city").
		MinAs("amount", "lo").
		MaxAs("amount", "hi").
		AvgAs("amount", "mean").
		Having("hi", ">", 100).
		Aggregate(ctx, &rows)
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty having result = %+v, %v", rows, err)
	}
}

func TestGroupByMultipleFieldsAggregate(t *testing.T) {
	engine, ctx := openTestEngine(t)
	seedOrders(t, engine, ctx)

	type row struct {
		City   string `bson:"city"`
		Status string `bson:"status"`
		N      int64  `bson:"n"`
	}
	var rows []row
	err := engine.Collection("orders").
		GroupBy("city", "status").
		CountAs("n").
		OrderBy("city").
		OrderBy("status").
		Aggregate(ctx, &rows)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(rows) != 4 || rows[0].City != "BJ" || rows[0].Status != "paid" || rows[0].N != 1 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestCursorEach(t *testing.T) {
	engine, ctx := openTestEngine(t)
	seedOrders(t, engine, ctx)

	var cities []string
	err := engine.Collection("orders").
		Where("status", "paid").
		OrderBy("amount", "desc").
		Each(ctx, func(c *Cursor) error {
			var o testOrder
			if err := c.Decode(&o); err != nil {
				return err
			}
			cities = append(cities, o.City)
			return nil
		})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(cities) != 4 || cities[0] != "SH" || cities[3] != "BJ" {
		t.Fatalf("Each order = %v", cities)
	}

	sentinel := errors.New("stop")
	seen := 0
	err = engine.Collection("orders").Each(ctx, func(c *Cursor) error {
		seen++
		if seen == 2 {
			return sentinel
		}
		return nil
	})
	if !errors.Is(err, sentinel) || seen != 2 {
		t.Fatalf("Each early stop: seen=%d err=%v", seen, err)
	}
}

func TestCursorManualIteration(t *testing.T) {
	engine, ctx := openTestEngine(t)
	seedOrders(t, engine, ctx)

	cursor, err := engine.Collection("orders").OrderBy("_id").Limit(2).Cursor(ctx)
	if err != nil {
		t.Fatalf("Cursor: %v", err)
	}
	defer cursor.Close(ctx)
	var ids []int64
	for cursor.Next(ctx) {
		var o testOrder
		if err := cursor.Decode(&o); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if cursor.Current().Lookup("_id").AsInt64() != o.ID {
			t.Fatalf("Current mismatch: %v vs %d", cursor.Current(), o.ID)
		}
		ids = append(ids, o.ID)
	}
	if err := cursor.Err(); err != nil {
		t.Fatalf("cursor.Err: %v", err)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("ids = %v", ids)
	}

	if _, err := engine.Collection("").Cursor(ctx); !errors.Is(err, ErrCollectionRequired) {
		t.Fatalf("Cursor without collection = %v", err)
	}
}

func TestGroupFlattenedKeyCollision(t *testing.T) {
	if _, err := (&Query{}).GroupBy("addr.city", "addr_city").buildGroupPipeline(); err == nil {
		t.Fatal("flattened key collision must error")
	}
	if _, err := (&Query{}).GroupBy("city", "city").buildGroupPipeline(); err == nil {
		t.Fatal("duplicate group key must error")
	}
}

func TestGroupResultColumnValidation(t *testing.T) {
	if _, err := (&Query{}).GroupBy("city").CountAs("n").Having("totl", ">", 1).buildGroupPipeline(); err == nil {
		t.Fatal("having on unknown column must error")
	}
	if _, err := (&Query{}).GroupBy("addr.city").OrderBy("addr.city").buildGroupPipeline(); err == nil {
		t.Fatal("order by on raw dotted field must error (use flattened name)")
	}
	if _, err := (&Query{}).GroupBy("addr.city").OrderBy("addr_city").buildGroupPipeline(); err != nil {
		t.Fatalf("order by flattened key should pass: %v", err)
	}
	if _, err := (&Query{}).GroupBy("city").Select("city").buildGroupPipeline(); err == nil {
		t.Fatal("Select combined with GroupBy must error")
	}
	if _, err := (&Query{}).GroupBy("city").Having("city", "Amy").buildGroupPipeline(); err != nil {
		t.Fatalf("having on group key should pass: %v", err)
	}
}

func TestSingleDottedGroupKeyProjection(t *testing.T) {
	q := (&Query{}).GroupBy("addr.city")
	pipeline, err := q.buildGroupPipeline()
	if err != nil {
		t.Fatalf("buildGroupPipeline: %v", err)
	}
	if id := pipeline[0].(bson.M)["$group"].(bson.M)["_id"]; id != "$addr.city" {
		t.Fatalf("group _id = %#v", id)
	}
	if p := pipeline[1].(bson.M)["$project"].(bson.M); p["addr_city"] != "$_id" {
		t.Fatalf("project stage = %#v", p)
	}
}

func TestAggAliasSyntaxValidation(t *testing.T) {
	if q := (&Query{}).GroupBy("city").CountAs("$n"); q.err == nil {
		t.Fatal("alias starting with $ must error")
	}
	if q := (&Query{}).GroupBy("city").SumAs("amount", "a.b"); q.err == nil {
		t.Fatal("alias containing dot must error")
	}
}

func TestOrWhereEmptyMapIsNoop(t *testing.T) {
	q := (&Query{}).Where("a", 1).OrWhere(bson.M{})
	filter := q.buildFilter()
	if _, hasOr := filter["$or"]; hasOr || filter["a"] != 1 {
		t.Fatalf("empty OrWhere map must be a no-op: %#v", filter)
	}
	q = (&Query{}).OrWhere(bson.M{})
	if filter := q.buildFilter(); len(filter) != 0 {
		t.Fatalf("filter = %#v", filter)
	}
}

func TestGroupStateRejectedByNonAggregate(t *testing.T) {
	engine, ctx := openTestEngine(t)
	seedOrders(t, engine, ctx)

	var out []testOrder
	if err := engine.Collection("orders").GroupBy("city").Find(ctx, &out); err == nil {
		t.Fatal("Find with pending GroupBy must error")
	}
	if _, err := engine.Collection("orders").GroupBy("city").CountAs("n").Count(ctx); err == nil {
		t.Fatal("Count with pending group state must error")
	}
	if _, err := engine.Collection("orders").Having("n", ">", 1).Delete(ctx); err == nil {
		t.Fatal("Delete with pending Having must error")
	}
	if _, err := engine.Collection("orders").GroupBy("city").Cursor(ctx); err == nil {
		t.Fatal("Cursor with pending GroupBy must error")
	}
}

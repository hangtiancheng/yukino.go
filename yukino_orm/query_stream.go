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

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Cursor streams query results one document at a time without loading the
// full result set into memory. Callers must Close it (Each does so
// automatically).
type Cursor struct {
	cursor *mongo.Cursor
	engine *Engine
}

// Cursor executes the query and returns a streaming cursor honoring the
// filter, sort, limit, offset, and projection of the Query.
func (q *Query) Cursor(ctx context.Context) (*Cursor, error) {
	if err := q.preflight(); err != nil {
		return nil, err
	}
	ctx = q.execCtx(ctx)
	cursor, err := q.collection.Find(ctx, q.buildFilter(), q.findOptions())
	if err != nil {
		return nil, err
	}
	return &Cursor{cursor: cursor, engine: q.engine}, nil
}

// Each streams every matching document through fn, stopping at the first
// error, which is returned. The cursor is closed automatically.
func (q *Query) Each(ctx context.Context, fn func(c *Cursor) error) error {
	cursor, err := q.Cursor(ctx)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		if err := fn(cursor); err != nil {
			return err
		}
	}
	return cursor.Err()
}

// Next advances to the next document, returning false at the end of the
// stream or on error (check Err afterwards).
func (c *Cursor) Next(ctx context.Context) bool {
	return c.cursor.Next(c.bind(ctx))
}

// Decode unmarshals the current document into out.
func (c *Cursor) Decode(out any) error {
	return c.cursor.Decode(out)
}

// Current returns the raw BSON of the current document.
func (c *Cursor) Current() bson.Raw {
	return c.cursor.Current
}

// Err reports the error, if any, that terminated iteration.
func (c *Cursor) Err() error {
	return c.cursor.Err()
}

// Close releases the server-side cursor.
func (c *Cursor) Close(ctx context.Context) error {
	return c.cursor.Close(c.bind(ctx))
}

// bind keeps getMore/killCursors on the transaction session when the Cursor
// was opened through a Transaction sub-Engine.
func (c *Cursor) bind(ctx context.Context) context.Context {
	if c.engine == nil {
		return ctx
	}
	return c.engine.sessionContext(ctx)
}

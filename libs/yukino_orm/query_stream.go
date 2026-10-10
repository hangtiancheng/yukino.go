package yukino_orm

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Cursor struct {
	cursor *mongo.Cursor
	engine *Engine
}

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

func (c *Cursor) Next(ctx context.Context) bool {
	return c.cursor.Next(c.bind(ctx))
}

func (c *Cursor) Decode(out any) error {
	return c.cursor.Decode(out)
}

func (c *Cursor) Current() bson.Raw {
	return c.cursor.Current
}

func (c *Cursor) Err() error {
	return c.cursor.Err()
}

func (c *Cursor) Close(ctx context.Context) error {
	return c.cursor.Close(c.bind(ctx))
}

func (c *Cursor) bind(ctx context.Context) context.Context {
	if c.engine == nil {
		return ctx
	}
	return c.engine.sessionContext(ctx)
}

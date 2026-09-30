package yukino_orm

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Engine struct {
	client       *mongo.Client
	database     *mongo.Database
	databaseName string
	session      *mongo.Session
}

func NewEngine(ctx context.Context, uri string, database string) (*Engine, error) {
	if strings.TrimSpace(uri) == "" {
		return nil, errors.New("mongo uri is required")
	}
	if strings.TrimSpace(database) == "" {
		return nil, errors.New("mongo database is required")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}
	return &Engine{
		client:       client,
		database:     client.Database(database),
		databaseName: database,
	}, nil
}

func (e *Engine) Client() *mongo.Client {
	if e == nil {
		return nil
	}
	return e.client
}

func (e *Engine) Database() *mongo.Database {
	if e == nil {
		return nil
	}
	return e.database
}

func (e *Engine) DatabaseName() string {
	if e == nil {
		return ""
	}
	return e.databaseName
}

func (e *Engine) Collection(name string) *Query {
	var col *mongo.Collection
	if e != nil && e.database != nil && strings.TrimSpace(name) != "" {
		col = e.database.Collection(name)
	}
	return &Query{collection: col, engine: e}
}

func (e *Engine) Model(value any) *Query {
	return e.Collection(CollectionName(value))
}

func (e *Engine) Close(ctx context.Context) error {
	if e == nil || e.client == nil {
		return nil
	}
	return e.client.Disconnect(ctx)
}

func (e *Engine) DropDatabase(ctx context.Context) error {
	if e == nil || e.database == nil {
		return nil
	}
	return e.database.Drop(ctx)
}

func (e *Engine) NextSequence(ctx context.Context, name string) (int64, error) {
	if e == nil || e.database == nil {
		return 0, errors.New("engine is not initialized")
	}
	if strings.TrimSpace(name) == "" {
		return 0, errors.New("sequence name is required")
	}
	counters := e.database.Collection("counters")
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	update := bson.M{"$inc": bson.M{"value": int64(1)}}
	var result struct {
		Value int64 `bson:"value"`
	}
	if err := counters.FindOneAndUpdate(e.sessionContext(ctx), bson.M{"_id": name}, update, opts).Decode(&result); err != nil {
		return 0, err
	}
	return result.Value, nil
}

// sessionContext binds the engine's transaction session to ctx so that
// operations join the transaction even when a plain context is passed. A
// context that already carries a session is returned unchanged.
func (e *Engine) sessionContext(ctx context.Context) context.Context {
	if e == nil || e.session == nil {
		return ctx
	}
	if mongo.SessionFromContext(ctx) != nil {
		return ctx
	}
	return mongo.NewSessionContext(ctx, e.session)
}

func (e *Engine) Transaction(ctx context.Context, fn func(sc context.Context, tx *Engine) error) error {
	if e == nil || e.client == nil {
		return errors.New("engine is not initialized")
	}
	session, err := e.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(sc context.Context) (any, error) {
		txEngine := &Engine{
			client:       e.client,
			database:     e.database,
			databaseName: e.databaseName,
			session:      session,
		}
		return nil, fn(sc, txEngine)
	})
	return err
}

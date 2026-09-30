package dao

import (
	"context"
	"log"

	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/model"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func uniqueUuidIndex() mongo.IndexModel {
	return mongo.IndexModel{
		Keys:    bson.D{{Key: "uuid", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
}

func compoundIndex(fields ...string) mongo.IndexModel {
	keys := bson.D{}
	for _, f := range fields {
		keys = append(keys, bson.E{Key: f, Value: 1})
	}
	return mongo.IndexModel{Keys: keys}
}

// InitIndexes creates the indexes the query paths rely on. The unique uuid
// index also guards against random-id collisions.
func InitIndexes() {
	ctx := context.Background()
	specs := []struct {
		model   any
		indexes []mongo.IndexModel
	}{
		{&model.UserInfo{}, []mongo.IndexModel{
			uniqueUuidIndex(),
			compoundIndex("telephone"),
		}},
		{&model.GroupInfo{}, []mongo.IndexModel{
			uniqueUuidIndex(),
			compoundIndex("owner_id"),
		}},
		{&model.Session{}, []mongo.IndexModel{
			uniqueUuidIndex(),
			compoundIndex("send_id", "receive_id"),
			compoundIndex("receive_id"),
		}},
		{&model.Message{}, []mongo.IndexModel{
			uniqueUuidIndex(),
			compoundIndex("send_id", "receive_id", "created_at"),
			compoundIndex("receive_id", "created_at"),
		}},
		{&model.UserContact{}, []mongo.IndexModel{
			compoundIndex("user_id", "contact_id"),
			compoundIndex("contact_id"),
		}},
		{&model.ContactApply{}, []mongo.IndexModel{
			uniqueUuidIndex(),
			compoundIndex("contact_id", "status"),
			compoundIndex("user_id", "contact_id"),
		}},
		{&model.ContactTag{}, []mongo.IndexModel{
			uniqueUuidIndex(),
			compoundIndex("user_id"),
		}},
	}
	for _, spec := range specs {
		if _, err := Engine.Model(spec.model).EnsureIndexes(ctx, spec.indexes); err != nil {
			log.Printf("ensure indexes for %T failed: %v", spec.model, err)
		}
	}
}

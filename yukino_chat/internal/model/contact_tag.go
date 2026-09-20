package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type ContactTag struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"-"`
	Uuid      string        `bson:"uuid" json:"uuid"`
	UserId    string        `bson:"user_id" json:"user_id"`
	Name      string        `bson:"name" json:"name"`
	CreatedAt time.Time     `bson:"created_at" json:"created_at"`
	DeletedAt *time.Time    `bson:"deleted_at,omitempty" json:"-"`
}

package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type ContactApply struct {
	ID          bson.ObjectID `bson:"_id,omitempty" json:"-"`
	Uuid        string        `bson:"uuid" json:"uuid"`
	UserId      string        `bson:"user_id" json:"user_id"`
	ContactId   string        `bson:"contact_id" json:"contact_id"`
	ContactType int8          `bson:"contact_type" json:"contact_type"`
	Status      int8          `bson:"status" json:"status"`
	Message     string        `bson:"message" json:"message"`
	LastApplyAt time.Time     `bson:"last_apply_at" json:"last_apply_at"`
	DeletedAt   *time.Time    `bson:"deleted_at,omitempty" json:"-"`
}

package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type UserContact struct {
	ID          bson.ObjectID `bson:"_id,omitempty" json:"-"`
	UserId      string        `bson:"user_id" json:"user_id"`
	ContactId   string        `bson:"contact_id" json:"contact_id"`
	ContactType int8          `bson:"contact_type" json:"contact_type"`
	Status      int8          `bson:"status" json:"status"`
	NoteName    string        `bson:"note_name" json:"note_name"`
	TagId       string        `bson:"tag_id" json:"tag_id"`
	CreatedAt   time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time     `bson:"updated_at" json:"updated_at"`
	DeletedAt   *time.Time    `bson:"deleted_at,omitempty" json:"-"`
}

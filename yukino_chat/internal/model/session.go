package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Session struct {
	ID            bson.ObjectID `bson:"_id,omitempty" json:"-"`
	Uuid          string        `bson:"uuid" json:"uuid"`
	SendId        string        `bson:"send_id" json:"send_id"`
	ReceiveId     string        `bson:"receive_id" json:"receive_id"`
	ReceiveName   string        `bson:"receive_name" json:"receive_name"`
	Avatar        string        `bson:"avatar" json:"avatar"`
	LastMessage   string        `bson:"last_message" json:"last_message"`
	LastMessageAt *time.Time    `bson:"last_message_at,omitempty" json:"last_message_at"`
	LastReadAt    *time.Time    `bson:"last_read_at,omitempty" json:"last_read_at"`
	CreatedAt     time.Time     `bson:"created_at" json:"created_at"`
	DeletedAt     *time.Time    `bson:"deleted_at,omitempty" json:"-"`
}

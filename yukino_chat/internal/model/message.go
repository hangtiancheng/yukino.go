package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Message struct {
	ID         bson.ObjectID `bson:"_id,omitempty" json:"-"`
	Uuid       string        `bson:"uuid" json:"uuid"`
	SessionId  string        `bson:"session_id" json:"session_id"`
	Type       int8          `bson:"type" json:"type"`
	Content    string        `bson:"content" json:"content"`
	Url        string        `bson:"url" json:"url"`
	SendId     string        `bson:"send_id" json:"send_id"`
	SendName   string        `bson:"send_name" json:"send_name"`
	SendAvatar string        `bson:"send_avatar" json:"send_avatar"`
	ReceiveId  string        `bson:"receive_id" json:"receive_id"`
	FileType   string        `bson:"file_type" json:"file_type"`
	FileName   string        `bson:"file_name" json:"file_name"`
	FileSize   string        `bson:"file_size" json:"file_size"`
	Status     int8          `bson:"status" json:"status"`
	CreatedAt  time.Time     `bson:"created_at" json:"created_at"`
	SendAt     *time.Time    `bson:"send_at,omitempty" json:"send_at"`
	AVdata     string        `bson:"av_data" json:"av_data"`
}

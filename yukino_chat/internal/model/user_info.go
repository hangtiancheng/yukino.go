package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type UserInfo struct {
	ID            bson.ObjectID `bson:"_id,omitempty" json:"-"`
	Uuid          string        `bson:"uuid" json:"uuid"`
	Nickname      string        `bson:"nickname" json:"nickname"`
	Telephone     string        `bson:"telephone" json:"telephone"`
	Email         string        `bson:"email" json:"email"`
	Avatar        string        `bson:"avatar" json:"avatar"`
	Gender        int8          `bson:"gender" json:"gender"`
	Signature     string        `bson:"signature" json:"signature"`
	Password      string        `bson:"password" json:"-"`
	Birthday      string        `bson:"birthday" json:"birthday"`
	CreatedAt     time.Time     `bson:"created_at" json:"created_at"`
	DeletedAt     *time.Time    `bson:"deleted_at,omitempty" json:"-"`
	LastOnlineAt  *time.Time    `bson:"last_online_at,omitempty" json:"last_online_at"`
	LastOfflineAt *time.Time    `bson:"last_offline_at,omitempty" json:"last_offline_at"`
	IsAdmin       int8          `bson:"is_admin" json:"is_admin"`
	Status        int8          `bson:"status" json:"status"`
}

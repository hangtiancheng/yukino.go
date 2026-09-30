package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type GroupInfo struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"-"`
	Uuid      string        `bson:"uuid" json:"uuid"`
	Name      string        `bson:"name" json:"name"`
	Notice    string        `bson:"notice" json:"notice"`
	Members   []string      `bson:"members" json:"members"`
	MemberCnt int           `bson:"member_cnt" json:"member_cnt"`
	OwnerId   string        `bson:"owner_id" json:"owner_id"`
	AddMode   int8          `bson:"add_mode" json:"add_mode"`
	Avatar    string        `bson:"avatar" json:"avatar"`
	Status    int8          `bson:"status" json:"status"`
	CreatedAt time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at" json:"updated_at"`
	DeletedAt *time.Time    `bson:"deleted_at,omitempty" json:"-"`
}

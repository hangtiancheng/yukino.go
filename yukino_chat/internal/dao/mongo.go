package dao

import (
	"context"
	"log"

	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/config"

	"github.com/hangtiancheng/yukino.go/yukino_orm"
)

var Engine *yukino_orm.Engine

func InitMongo() {
	conf := config.Get()
	var err error
	Engine, err = yukino_orm.NewEngine(context.Background(), conf.Mongo.URI, conf.Mongo.Database)
	if err != nil {
		log.Fatalf("failed to connect mongo: %v", err)
	}
	log.Printf("connected to mongodb: %s/%s", conf.Mongo.URI, conf.Mongo.Database)
}

func CloseMongo() {
	if Engine != nil {
		_ = Engine.Close(context.Background())
	}
}

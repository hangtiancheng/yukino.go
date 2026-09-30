package dao

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/config"
	"github.com/hangtiancheng/yukino.go/yukino_chat/internal/model"

	"github.com/hangtiancheng/yukino.go/yukino_cache"
)

var (
	// UserInfoCache caches user documents by uuid (read-through).
	UserInfoCache *yukino_cache.Group
	// SessionListCache caches each user's active session list by owner id
	// (read-through). Both the user and group session endpoints share it.
	SessionListCache *yukino_cache.Group
)

func InitCache() {
	conf := config.Get()
	maxBytes := conf.Cache.MaxBytes
	expiration := time.Duration(conf.Cache.Expiration) * time.Second
	opts := []yukino_cache.GroupOption{yukino_cache.WithExpiration(expiration)}

	UserInfoCache = yukino_cache.NewGroup("user_info", maxBytes, yukino_cache.GetterFunc(
		func(ctx context.Context, key string) ([]byte, error) {
			if key == "" {
				return nil, yukino_cache.ErrKeyRequired
			}
			var user model.UserInfo
			if err := ActiveQuery(&user).Where("uuid", key).First(ctx, &user); err != nil {
				return nil, err
			}
			return json.Marshal(&user)
		},
	), opts...)

	SessionListCache = yukino_cache.NewGroup("session_list", maxBytes, yukino_cache.GetterFunc(
		func(ctx context.Context, key string) ([]byte, error) {
			if key == "" {
				return nil, yukino_cache.ErrKeyRequired
			}
			var sessions []model.Session
			if err := ActiveQuery(&sessions).
				Where("send_id", key).
				OrderBy("created_at", "desc").
				Find(ctx, &sessions); err != nil {
				return nil, err
			}
			return json.Marshal(sessions)
		},
	), opts...)

	log.Printf("cache initialized: maxBytes=%d, expiration=%v", maxBytes, expiration)
}

func CloseCache() {
	yukino_cache.DestroyAllGroups()
}

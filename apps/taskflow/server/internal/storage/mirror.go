package storage

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/hangtiancheng/yukino.go/apps/taskflow/server/internal/model/po"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type mirrorCursor struct {
	LastID uint   `json:"last_id"`
	MaxID  uint   `json:"max_id"`
	Prune  uint64 `json:"prune"`
}

type mirrorValue struct {
	Status    string `json:"status"`
	FireKey   string `json:"fire_key"`
	TaskType  string `json:"task_type"`
	UpdatedAt string `json:"updated_at"`
}

const mirrorWrite = `local old=redis.call('HGET',KEYS[1],ARGV[1])
if old then
 local ok,v=pcall(cjson.decode,old)
 if ok and v.updated_at and v.updated_at > ARGV[2] then return 0 end
end
return redis.call('HSET',KEYS[1],ARGV[1],ARGV[3])`

func SyncExecutionMirror(ctx context.Context, db *gorm.DB, client redis.UniversalClient, mirrorKey, cursorKey string, limit int) (int, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	var cursor mirrorCursor
	if raw, err := client.Get(ctx, cursorKey).Result(); err == nil {
		_ = json.Unmarshal([]byte(raw), &cursor)
	} else if err != redis.Nil {
		return 0, err
	}
	if cursor.MaxID == 0 {
		if err := db.WithContext(ctx).Model(&po.Execution{}).Select("COALESCE(MAX(id), 0)").Scan(&cursor.MaxID).Error; err != nil {
			return 0, err
		}
	}
	var rows []po.Execution
	if err := db.WithContext(ctx).Select("id", "status", "fire_key", "task_type", "updated_at").Where("id > ? AND id <= ?", cursor.LastID, cursor.MaxID).Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
	pipe := client.Pipeline()
	for _, row := range rows {
		cursor.LastID = row.ID
		if row.UpdatedAt.Before(cutoff) && terminal(row.Status) {
			continue
		}
		value := mirrorValue{row.Status, row.FireKey, row.TaskType, row.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")}
		body, _ := json.Marshal(value)
		pipe.Eval(ctx, mirrorWrite, []string{mirrorKey}, strconv.FormatUint(uint64(row.ID), 10), value.UpdatedAt, string(body))
	}
	pipe.Expire(ctx, mirrorKey, 7*24*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	if len(rows) < limit || cursor.LastID >= cursor.MaxID {
		cursor.LastID = 0
		cursor.MaxID = 0
	}
	fields, next, err := client.HScan(ctx, mirrorKey, cursor.Prune, "*", 200).Result()
	if err != nil {
		return len(rows), err
	}
	for i := 0; i+1 < len(fields); i += 2 {
		var value mirrorValue
		if json.Unmarshal([]byte(fields[i+1]), &value) != nil {
			continue
		}
		updated, _ := time.Parse(time.RFC3339Nano, value.UpdatedAt)
		if terminal(value.Status) && updated.Before(cutoff) {
			if err := client.Eval(ctx, `if redis.call('HGET',KEYS[1],ARGV[1]) == ARGV[2] then return redis.call('HDEL',KEYS[1],ARGV[1]) end return 0`, []string{mirrorKey}, fields[i], fields[i+1]).Err(); err != nil {
				return len(rows), err
			}
		}
	}
	cursor.Prune = next
	body, _ := json.Marshal(cursor)
	return len(rows), client.Set(ctx, cursorKey, body, 7*24*time.Hour).Err()
}

func terminal(status string) bool {
	return status == po.StatusSucceeded || status == po.StatusFailed || status == po.StatusCancelled
}

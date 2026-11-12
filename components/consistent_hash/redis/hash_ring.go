package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/distributed_lock"
	"github.com/hangtiancheng/yukino.go/components/distributed_lock/utils"
	go_redis "github.com/redis/go-redis/v9"
)

type RedisHashRing struct {
	key         string
	redisClient *Client
	lockMu      sync.Mutex
	locks       map[string]*distributed_lock.RedisLock
}

func NewRedisHashRing(key string, redisClient *Client) *RedisHashRing {
	return &RedisHashRing{
		key:         key,
		redisClient: redisClient,
		locks:       make(map[string]*distributed_lock.RedisLock),
	}
}

func (r *RedisHashRing) getLockKey() string {
	return fmt.Sprintf("redis:consistent_hash:ring:lock:%s", r.key)
}

func (r *RedisHashRing) getTableKey() string {
	return fmt.Sprintf("redis:consistent_hash:ring:%s", r.key)
}

func (r *RedisHashRing) getNodeReplicaKey() string {
	return fmt.Sprintf("redis:consistent_hash:ring:node:replica:%s", r.key)
}

func (r *RedisHashRing) getNodeDataKey(nodeID string) string {
	return fmt.Sprintf("redis:consistent_hash:ring:%s:node:data:%s", r.key, nodeID)
}

func (r *RedisHashRing) Lock(ctx context.Context, expireSeconds int) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	lock := distributed_lock.NewRedisLock(r.getLockKey(), r.redisClient, distributed_lock.WithExpireSeconds(int64(expireSeconds)))
	if err := lock.Lock(ctx); err != nil {
		return err
	}
	r.lockMu.Lock()
	r.locks[utils.GetProcessAndGoroutineIDStr()] = lock
	r.lockMu.Unlock()
	return nil
}

func (r *RedisHashRing) Unlock(ctx context.Context) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	owner := utils.GetProcessAndGoroutineIDStr()
	r.lockMu.Lock()
	lock := r.locks[owner]
	r.lockMu.Unlock()
	if lock == nil {
		return errors.New("redis hash ring lock is not owned by this caller")
	}
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := lock.Unlock(unlockCtx); err != nil {
		return err
	}
	r.lockMu.Lock()
	delete(r.locks, owner)
	r.lockMu.Unlock()
	return nil
}

func (r *RedisHashRing) Add(ctx context.Context, score int32, nodeID string) error {
	scoreEntities, err := r.redisClient.ZRangeByScore(ctx, r.getTableKey(), int64(score), int64(score))
	if err != nil {
		return fmt.Errorf("redis ring add failed, err: %w", err)
	}

	if len(scoreEntities) > 1 {
		return fmt.Errorf("invalid score entity len: %d", len(scoreEntities))
	}

	var nodeIDs []string
	if len(scoreEntities) == 1 {
		if err = json.Unmarshal([]byte(scoreEntities[0].Val), &nodeIDs); err != nil {
			return err
		}
		if slices.Contains(nodeIDs, nodeID) {
			return nil
		}

		if err = r.redisClient.ZRem(ctx, r.getTableKey(), scoreEntities[0].Score); err != nil {
			return fmt.Errorf("redis ring zrem failed, err: %w", err)
		}
	}

	nodeIDs = append(nodeIDs, nodeID)
	newNodeIDs, _ := json.Marshal(nodeIDs)
	if err = r.redisClient.ZAdd(ctx, r.getTableKey(), int64(score), string(newNodeIDs)); err != nil {
		return fmt.Errorf("redis ring zadd failed, err: %w", err)
	}
	return nil
}

func (r *RedisHashRing) Ceiling(ctx context.Context, score int32) (int32, error) {
	scoreEntity, err := r.redisClient.Ceiling(ctx, r.getTableKey(), int64(score))
	if err != nil && !errors.Is(err, ErrScoreNotExist) {
		return 0, fmt.Errorf("redis ring ceiling failed, err: %w", err)
	}

	if scoreEntity != nil {
		return int32(scoreEntity.Score), nil
	}

	if scoreEntity, err = r.redisClient.FirstOrLast(ctx, r.getTableKey(), true); err != nil && !errors.Is(err, ErrScoreNotExist) {
		return 0, fmt.Errorf("redis ring first failed, err: %w", err)
	}

	if scoreEntity != nil {
		return int32(scoreEntity.Score), nil
	}

	return -1, nil
}

func (r *RedisHashRing) Floor(ctx context.Context, score int32) (int32, error) {
	scoreEntity, err := r.redisClient.Floor(ctx, r.getTableKey(), int64(score))
	if err != nil && !errors.Is(err, ErrScoreNotExist) {
		return 0, fmt.Errorf("redis ring floor failed, err: %w", err)
	}

	if scoreEntity != nil {
		return int32(scoreEntity.Score), nil
	}

	if scoreEntity, err = r.redisClient.FirstOrLast(ctx, r.getTableKey(), false); err != nil && !errors.Is(err, ErrScoreNotExist) {
		return 0, fmt.Errorf("redis ring last failed, err: %w", err)
	}

	if scoreEntity != nil {
		return int32(scoreEntity.Score), nil
	}

	return -1, nil
}

func (r *RedisHashRing) Rem(ctx context.Context, score int32, nodeID string) error {
	scoreEntities, err := r.redisClient.ZRangeByScore(ctx, r.getTableKey(), int64(score), int64(score))
	if err != nil {
		return fmt.Errorf("redis ring rem zrange by score failed, err: %w", err)
	}

	if len(scoreEntities) != 1 {
		return fmt.Errorf("redis ring rem failed, invalid score entity len: %d", len(scoreEntities))
	}

	var nodeIDs []string
	if err = json.Unmarshal([]byte(scoreEntities[0].Val), &nodeIDs); err != nil {
		return err
	}

	index := -1
	for i := 0; i < len(nodeIDs); i++ {
		if nodeIDs[i] == nodeID {
			index = i
			break
		}
	}

	if index == -1 {
		return fmt.Errorf("node %q does not exist at score %d", nodeID, score)
	}

	if err = r.redisClient.ZRem(ctx, r.getTableKey(), scoreEntities[0].Score); err != nil {
		return fmt.Errorf("redis ring rem zrem failed, err: %w", err)
	}

	nodeIDs = append(nodeIDs[:index], nodeIDs[index+1:]...)
	if len(nodeIDs) == 0 {
		return nil
	}

	newNodeIDStr, _ := json.Marshal(nodeIDs)
	if err = r.redisClient.ZAdd(ctx, r.getTableKey(), scoreEntities[0].Score, string(newNodeIDStr)); err != nil {
		return fmt.Errorf("redis ring rem zadd failed, err: %w", err)
	}
	return nil
}

func (r *RedisHashRing) Nodes(ctx context.Context) (map[string]int, error) {
	rawData, err := r.redisClient.HGetAll(ctx, r.getNodeReplicaKey())
	if err != nil {
		return nil, fmt.Errorf("redis ring nodes hgetall failed, err: %w", err)
	}
	data := make(map[string]int, len(rawData))
	for rawKey, rawVal := range rawData {
		replicas, err := strconv.Atoi(rawVal)
		if err != nil || replicas <= 0 {
			return nil, fmt.Errorf("invalid replica count for node %q: %q", rawKey, rawVal)
		}
		data[rawKey] = replicas
	}
	return data, nil
}

func (r *RedisHashRing) AddNodeToReplica(ctx context.Context, nodeID string, replicas int) error {
	if err := r.redisClient.HSet(ctx, r.getNodeReplicaKey(), nodeID, strconv.Itoa(replicas)); err != nil {
		return fmt.Errorf("redis ring add node to replica failed, err: %w", err)
	}
	return nil
}

func (r *RedisHashRing) DeleteNodeToReplica(ctx context.Context, nodeID string) error {
	if err := r.redisClient.HDel(ctx, r.getNodeReplicaKey(), nodeID); err != nil {
		return fmt.Errorf("redis ring delete node to replica failed, err: %w", err)
	}
	return nil
}

func (r *RedisHashRing) Node(ctx context.Context, score int32) ([]string, error) {
	scoreEntities, err := r.redisClient.ZRangeByScore(ctx, r.getTableKey(), int64(score), int64(score))
	if err != nil {
		return nil, fmt.Errorf("redis ring node zrange by score failed, err: %w", err)
	}

	if len(scoreEntities) != 1 {
		return nil, fmt.Errorf("redis ring node failed, invalid len of score entities: %d", len(scoreEntities))
	}

	var nodeIDs []string
	if err = json.Unmarshal([]byte(scoreEntities[0].Val), &nodeIDs); err != nil {
		return nil, err
	}

	return nodeIDs, nil
}

func (r *RedisHashRing) DataKeys(ctx context.Context, nodeID string) (map[string]struct{}, error) {
	resStr, err := r.redisClient.Get(ctx, r.getNodeDataKey(nodeID))
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return nil, fmt.Errorf("redis ring dataKeys get failed, err: %w", err)
	}

	dataKeys := make(map[string]struct{})
	if len(resStr) > 0 {
		if err = json.Unmarshal([]byte(resStr), &dataKeys); err != nil {
			return nil, err
		}
	}

	return dataKeys, nil
}

func (r *RedisHashRing) AddNodeToDataKeys(ctx context.Context, nodeID string, dataKeys map[string]struct{}) error {
	resStr, err := r.redisClient.Get(ctx, r.getNodeDataKey(nodeID))
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return fmt.Errorf("redis ring addNodeToDataKey get failed, err: %w", err)
	}

	var oldDataKeys map[string]struct{}
	if len(resStr) > 0 {
		if err = json.Unmarshal([]byte(resStr), &oldDataKeys); err != nil {
			return err
		}
	}

	if oldDataKeys == nil {
		oldDataKeys = make(map[string]struct{})
	}
	for dataKey := range dataKeys {
		oldDataKeys[dataKey] = struct{}{}
	}

	dataKeysStr, _ := json.Marshal(oldDataKeys)
	if err = r.redisClient.Set(ctx, r.getNodeDataKey(nodeID), string(dataKeysStr)); err != nil {
		return fmt.Errorf("redis ring addNodeToDataKey set failed, err: %w", err)
	}
	return nil
}

func (r *RedisHashRing) DeleteNodeToDataKeys(ctx context.Context, nodeID string, dataKeys map[string]struct{}) error {
	resStr, err := r.redisClient.Get(ctx, r.getNodeDataKey(nodeID))
	if err != nil && !errors.Is(err, go_redis.Nil) {
		return fmt.Errorf("redis ring deleteNodeToDataKey get failed, err: %w", err)
	}

	var oldDataKeys map[string]struct{}
	if len(resStr) > 0 {
		if err = json.Unmarshal([]byte(resStr), &oldDataKeys); err != nil {
			return err
		}
	}

	for dataKey := range dataKeys {
		delete(oldDataKeys, dataKey)
	}

	if len(oldDataKeys) == 0 {
		return r.redisClient.Del(ctx, r.getNodeDataKey(nodeID))
	}

	newDataKeyStr, _ := json.Marshal(oldDataKeys)
	return r.redisClient.Set(ctx, r.getNodeDataKey(nodeID), string(newDataKeyStr))
}

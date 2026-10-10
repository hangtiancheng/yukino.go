package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/components/consistent_hash"
	"github.com/redis/go-redis/v9"
)

const (
	ringKeyPrefix      = "taskflow:ring"
	heartbeatKeyPrefix = "taskflow:node:"
	heartbeatTTL       = 45 * time.Second
	heartbeatInterval  = 15 * time.Second
)

type SingletonRegistry struct {
	ring     *consistent_hash.ConsistentHash
	hashRing consistent_hash.HashRing
	client   redis.UniversalClient
	nodeID   string

	stopOnce sync.Once
	stopChan chan struct{}
	doneChan chan struct{}
}

func NewSingletonRegistry(ring *consistent_hash.ConsistentHash, hashRing consistent_hash.HashRing, client redis.UniversalClient, nodeID string) *SingletonRegistry {
	return &SingletonRegistry{
		ring:     ring,
		hashRing: hashRing,
		client:   client,
		nodeID:   nodeID,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

func (r *SingletonRegistry) Register(ctx context.Context) error {
	if err := r.beat(ctx); err != nil {
		return err
	}
	if err := r.ring.AddNode(ctx, r.nodeID, 1); err != nil {
		slog.Warn("ring AddNode", "node", r.nodeID, "err", err)
	}
	go r.loop()
	return nil
}

func (r *SingletonRegistry) Deregister(ctx context.Context) {
	r.stopOnce.Do(func() { close(r.stopChan) })
	<-r.doneChan

	if err := r.ring.RemoveNode(ctx, r.nodeID); err != nil {
		slog.Warn("ring RemoveNode", "node", r.nodeID, "err", err)
	}
	_ = r.client.Del(ctx, heartbeatKeyPrefix+r.nodeID).Err()
}

func (r *SingletonRegistry) loop() {
	defer close(r.doneChan)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := r.beat(ctx); err != nil {
				slog.Warn("heartbeat failed", "err", err)
			}
			if err := r.evictDeadNodes(ctx); err != nil {
				slog.Warn("evict dead nodes failed", "err", err)
			}
			cancel()
		}
	}
}

func (r *SingletonRegistry) beat(ctx context.Context) error {
	return r.client.Set(ctx, heartbeatKeyPrefix+r.nodeID, time.Now().Unix(), heartbeatTTL).Err()
}

func (r *SingletonRegistry) IsOwner(ctx context.Context, roleKey string) bool {
	node, err := r.ring.GetNode(ctx, roleKey)
	if err != nil {
		return true
	}
	return node == r.nodeID
}

func (r *SingletonRegistry) evictDeadNodes(ctx context.Context) error {
	nodes, err := r.hashRing.Nodes(ctx)
	if err != nil {
		return err
	}
	for nodeID := range nodes {
		if nodeID == r.nodeID {
			continue
		}
		exists, err := r.client.Exists(ctx, heartbeatKeyPrefix+nodeID).Result()
		if err != nil {
			return err
		}
		if exists == 0 {
			slog.Info("evicting dead node from ring", "node", nodeID)
			if err := r.ring.RemoveNode(ctx, nodeID); err != nil {
				slog.Warn("ring RemoveNode (evict)", "node", nodeID, "err", err)
			}
		}
	}
	return nil
}

func (r *SingletonRegistry) Members(ctx context.Context) map[string]any {
	result := map[string]any{
		"self": r.nodeID,
		"roles": map[string]string{
			SingletonMigrator: r.ownerOf(ctx, SingletonMigrator),
			SingletonMonitor:  r.ownerOf(ctx, SingletonMonitor),
		},
	}
	nodes, err := r.hashRing.Nodes(ctx)
	if err != nil {
		result["ring_error"] = err.Error()
		return result
	}
	members := make([]map[string]any, 0, len(nodes))
	for nodeID, weight := range nodes {
		alive, _ := r.client.Exists(ctx, heartbeatKeyPrefix+nodeID).Result()
		members = append(members, map[string]any{
			"node_id": nodeID,
			"weight":  weight,
			"alive":   alive > 0,
			"self":    nodeID == r.nodeID,
		})
	}
	result["nodes"] = members
	return result
}

func (r *SingletonRegistry) ownerOf(ctx context.Context, roleKey string) string {
	node, err := r.ring.GetNode(ctx, roleKey)
	if err != nil {
		return fmt.Sprintf("(unknown: %v)", err)
	}
	return node
}

func RingMigrator(ctx context.Context, dataKeys map[string]struct{}, from, to string) error {
	for key := range dataKeys {
		slog.Info("ring migration", "key", key, "from", from, "to", to)
	}
	return nil
}

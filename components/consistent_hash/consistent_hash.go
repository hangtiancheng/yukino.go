package consistent_hash

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type ConsistentHash struct {
	hashRing  HashRing
	migrator  Migrator
	encryptor Encryptor
	opts      ConsistentHashOptions
}

func NewConsistentHash(hashRing HashRing, encryptor Encryptor, migrator Migrator, opts ...ConsistentHashOption) *ConsistentHash {
	ch := ConsistentHash{
		hashRing:  hashRing,
		migrator:  migrator,
		encryptor: encryptor,
	}

	for _, opt := range opts {
		opt(&ch.opts)
	}

	repair(&ch.opts)
	return &ch
}

func (c *ConsistentHash) AddNode(ctx context.Context, nodeID string, weight int) error {
	if err := c.hashRing.Lock(ctx, c.opts.lockExpireSeconds); err != nil {
		return err
	}

	defer func() {
		_ = c.hashRing.Unlock(ctx)
	}()

	nodes, err := c.hashRing.Nodes(ctx)
	if err != nil {
		return err
	}

	for node := range nodes {
		if node == nodeID {
			return errors.New("repeat node")
		}
	}

	replicas := c.getValidWeight(weight) * c.opts.replicas
	if err = c.hashRing.AddNodeToReplica(ctx, nodeID, replicas); err != nil {
		return err
	}

	var migrateTasks []func()
	for i := range replicas {
		nodeKey := c.getRawNodeKey(nodeID, i)
		virtualScore := c.encryptor.Encrypt(nodeKey)

		if err := c.hashRing.Add(ctx, virtualScore, nodeKey); err != nil {
			return err
		}

		from, to, dataSet, err := c.migrateIn(ctx, virtualScore, nodeID)
		if err != nil {
			return err
		}

		if len(dataSet) == 0 {
			continue
		}

		migrateTasks = append(migrateTasks, func() {
			_ = c.migrator(ctx, dataSet, from, to)
		})
	}

	c.batchExecuteMigrator(migrateTasks)

	return nil
}

func (c *ConsistentHash) RemoveNode(ctx context.Context, nodeID string) error {
	if err := c.hashRing.Lock(ctx, c.opts.lockExpireSeconds); err != nil {
		return err
	}

	defer func() {
		_ = c.hashRing.Unlock(ctx)
	}()

	nodes, err := c.hashRing.Nodes(ctx)
	if err != nil {
		return err
	}

	var (
		nodeExist bool
		replicas  int
	)
	for node, _replicas := range nodes {
		if node == nodeID {
			nodeExist = true
			replicas = _replicas
			break
		}
	}

	if !nodeExist {
		return errors.New("invalid node id")
	}

	if err = c.hashRing.DeleteNodeToReplica(ctx, nodeID); err != nil {
		return err
	}

	var migrateTasks []func()
	for i := 0; i < replicas; i++ {
		virtualScore := c.encryptor.Encrypt(fmt.Sprintf("%s_%d", nodeID, i))
		from, to, dataSet, err := c.migrateOut(ctx, virtualScore, nodeID)
		if err != nil {
			return err
		}

		nodeKey := c.getRawNodeKey(nodeID, i)
		if err = c.hashRing.Rem(ctx, virtualScore, nodeKey); err != nil {
			return err
		}

		if len(dataSet) == 0 {
			continue
		}

		migrateTasks = append(migrateTasks, func() {
			_ = c.migrator(ctx, dataSet, from, to)
		})

	}

	c.batchExecuteMigrator(migrateTasks)

	return nil
}

func (c *ConsistentHash) batchExecuteMigrator(migrateTasks []func()) {
	var wg sync.WaitGroup
	for _, migrateTask := range migrateTasks {
		wg.Add(1)
		go func() {
			defer func() {
				if err := recover(); err != nil {
					slog.Error("migration task panicked", "panic", err)
				}
				wg.Done()
			}()
			migrateTask()
		}()
	}
	wg.Wait()
}

func (c *ConsistentHash) GetNode(ctx context.Context, dataKey string) (string, error) {
	if err := c.hashRing.Lock(ctx, c.opts.lockExpireSeconds); err != nil {
		return "", err
	}

	defer func() {
		_ = c.hashRing.Unlock(ctx)
	}()

	dataScore := c.encryptor.Encrypt(dataKey)
	ceilingScore, err := c.hashRing.Ceiling(ctx, dataScore)
	if err != nil {
		return "", err
	}

	if ceilingScore == -1 {
		return "", errors.New("no node available")
	}

	nodes, err := c.hashRing.Node(ctx, ceilingScore)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return "", errors.New("no node available with empty score")
	}

	nodeID := c.getNodeID(nodes[0])
	if err = c.hashRing.AddNodeToDataKeys(ctx, nodeID, map[string]struct{}{
		dataKey: {},
	}); err != nil {
		return "", err
	}

	return nodeID, nil
}

func (c *ConsistentHash) getValidWeight(weight int) int {
	if weight <= 0 {
		return 1
	}

	if weight >= 10 {
		return 10
	}

	return weight
}

func (c *ConsistentHash) getRawNodeKey(nodeID string, index int) string {
	return fmt.Sprintf("%s_%d", nodeID, index)
}

func (c *ConsistentHash) getNodeID(rawNodeKey string) string {
	index := strings.LastIndex(rawNodeKey, "_")
	if index < 0 {
		return rawNodeKey
	}
	return rawNodeKey[:index]
}

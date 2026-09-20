// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package consistent_hash

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// ConsistentHash implements consistent hashing on top of a redis zset-backed hash ring.
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

// AddNode adds a node and triggers data migration.
func (c *ConsistentHash) AddNode(ctx context.Context, nodeID string, weight int) error {
	// 1. Acquire the global distributed lock.
	if err := c.hashRing.Lock(ctx, c.opts.lockExpireSeconds); err != nil {
		return err
	}

	defer func() {
		_ = c.hashRing.Unlock(ctx)
	}()

	// 2. Reject duplicate nodes.
	nodes, err := c.hashRing.Nodes(ctx)
	if err != nil {
		return err
	}

	for node := range nodes {
		if node == nodeID {
			return errors.New("repeat node")
		}
	}

	// 3. Compute the virtual-node count from weight and replicas.
	replicas := c.getValidWeight(weight) * c.opts.replicas
	// 4. Persist the nodeID-to-replicas mapping so the node is marked as present.
	if err = c.hashRing.AddNodeToReplica(ctx, nodeID, replicas); err != nil {
		return err
	}

	var migrateTasks []func()
	for i := range replicas {
		// 5. Hash the i-th virtual node key to get its score on the ring.
		nodeKey := c.getRawNodeKey(nodeID, i)
		virtualScore := c.encryptor.Encrypt(nodeKey)

		// 6. Add the virtual node to the hash ring.
		if err := c.hashRing.Add(ctx, virtualScore, nodeKey); err != nil {
			return err
		}

		// 7. Determine which data keys must migrate to the new node.
		// from: the source node id.
		// to: the destination node id.
		// data: the data keys to migrate.
		from, to, dataSet, err := c.migrateIn(ctx, virtualScore, nodeID)
		if err != nil {
			return err
		}

		// Skip when there is nothing to migrate.
		if len(dataSet) == 0 {
			continue
		}

		// Defer migration so all tasks run in batch before returning.
		migrateTasks = append(migrateTasks, func() {
			_ = c.migrator(ctx, dataSet, from, to)
		})
	}

	c.batchExecuteMigrator(migrateTasks)

	return nil
}

// RemoveNode removes a node and triggers data migration.
// The caller learns which data must migrate and from/to which nodes.
func (c *ConsistentHash) RemoveNode(ctx context.Context, nodeID string) error {
	// 1. Acquire the global distributed lock.
	if err := c.hashRing.Lock(ctx, c.opts.lockExpireSeconds); err != nil {
		return err
	}

	defer func() {
		_ = c.hashRing.Unlock(ctx)
	}()

	// 2. Reject if the node does not exist.
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
	// 3. Iterate over all virtual nodes.
	for i := 0; i < replicas; i++ {
		// 4. Hash the i-th virtual node key to get its score on the ring.
		virtualScore := c.encryptor.Encrypt(fmt.Sprintf("%s_%d", nodeID, i))
		// 5. Determine migration targets, then remove the virtual node.
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

		// Defer migration so all tasks run in batch before returning.
		migrateTasks = append(migrateTasks, func() {
			_ = c.migrator(ctx, dataSet, from, to)
		})

	}

	c.batchExecuteMigrator(migrateTasks)

	return nil
}

func (c *ConsistentHash) batchExecuteMigrator(migrateTasks []func()) {
	// Execute all migration tasks concurrently.
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
	// 1. Acquire the global distributed lock.
	if err := c.hashRing.Lock(ctx, c.opts.lockExpireSeconds); err != nil {
		return "", err
	}

	defer func() {
		_ = c.hashRing.Unlock(ctx)
	}()

	// 1. Given a data key, find the node that owns it.
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

	// 2. Persist the data-key-to-node mapping.
	nodeID := c.getNodeID(nodes[0])
	if err = c.hashRing.AddNodeToDataKeys(ctx, nodeID, map[string]struct{}{
		dataKey: {},
	}); err != nil {
		return "", err
	}

	// Return the physical node id, consistent with the ownership index above,
	// so the result can be fed back into RemoveNode.
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

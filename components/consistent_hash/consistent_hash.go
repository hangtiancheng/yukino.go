package consistent_hash

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
)

type ConsistentHash struct {
	hashRing  HashRing
	migrator  Migrator
	encryptor Encryptor
	opts      ConsistentHashOptions
}

type migrationPlan struct {
	from    string
	to      string
	dataSet map[string]struct{}
}

func NewConsistentHash(hashRing HashRing, encryptor Encryptor, migrator Migrator, opts ...ConsistentHashOption) *ConsistentHash {
	ch := ConsistentHash{
		hashRing:  hashRing,
		migrator:  migrator,
		encryptor: encryptor,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(&ch.opts)
		}
	}

	repair(&ch.opts)
	return &ch
}

func (c *ConsistentHash) AddNode(ctx context.Context, nodeID string, weight int) error {
	if err := c.checkReady(ctx); err != nil {
		return err
	}
	if c.migrator == nil {
		return errors.New("node changes require a data migrator")
	}
	if nodeID == "" {
		return errors.New("empty node id")
	}
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

	weight = c.getValidWeight(weight)
	if c.opts.replicas > int(^uint(0)>>1)/weight {
		return errors.New("virtual node count overflows int")
	}
	replicas := weight * c.opts.replicas
	if replicas > 1<<20 {
		return errors.New("virtual node count exceeds the configured safety limit")
	}
	virtualNodes := make([]struct {
		key   string
		score int32
	}, replicas)
	for i := range replicas {
		nodeKey := c.getRawNodeKey(nodeID, i)
		score, err := c.hashScore(nodeKey)
		if err != nil {
			return err
		}
		virtualNodes[i] = struct {
			key   string
			score int32
		}{key: nodeKey, score: score}
	}
	if err = c.hashRing.AddNodeToReplica(ctx, nodeID, replicas); err != nil {
		return err
	}

	var migrateTasks []migrationPlan
	for _, virtualNode := range virtualNodes {
		nodeKey, virtualScore := virtualNode.key, virtualNode.score

		if err := c.hashRing.Add(ctx, virtualScore, nodeKey); err != nil {
			return err
		}

		from, to, dataSet, err := c.migrateIn(ctx, virtualScore, nodeID)
		if err != nil {
			return err
		}

		if len(dataSet) == 0 || from == to {
			continue
		}
		migrateTasks = append(migrateTasks, migrationPlan{from: from, to: to, dataSet: dataSet})
	}

	return c.batchExecuteMigrator(ctx, migrateTasks)
}

func (c *ConsistentHash) RemoveNode(ctx context.Context, nodeID string) error {
	if err := c.checkReady(ctx); err != nil {
		return err
	}
	if c.migrator == nil {
		return errors.New("node changes require a data migrator")
	}
	if nodeID == "" {
		return errors.New("empty node id")
	}
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
	virtualScores := make([]int32, replicas)
	for i := range replicas {
		score, err := c.hashScore(c.getRawNodeKey(nodeID, i))
		if err != nil {
			return err
		}
		virtualScores[i] = score
	}

	if err = c.hashRing.DeleteNodeToReplica(ctx, nodeID); err != nil {
		return err
	}

	var migrateTasks []migrationPlan
	for i, virtualScore := range virtualScores {
		from, to, dataSet, err := c.migrateOut(ctx, virtualScore, nodeID)
		if err != nil {
			return err
		}

		nodeKey := c.getRawNodeKey(nodeID, i)
		if err = c.hashRing.Rem(ctx, virtualScore, nodeKey); err != nil {
			return err
		}

		if len(dataSet) == 0 || from == to {
			continue
		}
		migrateTasks = append(migrateTasks, migrationPlan{from: from, to: to, dataSet: dataSet})

	}

	return c.batchExecuteMigrator(ctx, migrateTasks)
}

func (c *ConsistentHash) batchExecuteMigrator(ctx context.Context, migrateTasks []migrationPlan) error {
	type taskResult struct {
		index int
		err   error
	}
	var wg sync.WaitGroup
	results := make(chan taskResult, len(migrateTasks))
	for i, migrateTask := range migrateTasks {
		i, migrateTask := i, migrateTask
		wg.Add(1)
		go func() {
			var taskErr error
			defer func() {
				if fault := recover(); fault != nil {
					slog.Error("migration task panicked", "panic", fault)
					taskErr = fmt.Errorf("migration task panicked: %v", fault)
				}
				results <- taskResult{index: i, err: taskErr}
				wg.Done()
			}()
			taskErr = c.migrator(ctx, cloneDataSet(migrateTask.dataSet), migrateTask.from, migrateTask.to)
		}()
	}
	wg.Wait()
	close(results)
	taskErrors := make([]error, len(migrateTasks))
	for result := range results {
		taskErrors[result.index] = result.err
	}
	var migrationErr error
	for i, migrateTask := range migrateTasks {
		if taskErrors[i] != nil {
			migrationErr = errors.Join(migrationErr, taskErrors[i])
			continue
		}
		if err := c.transferDataKeys(ctx, migrateTask); err != nil {
			migrationErr = errors.Join(migrationErr, err)
		}
	}
	return migrationErr
}

func cloneDataSet(dataSet map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(dataSet))
	for key := range dataSet {
		cloned[key] = struct{}{}
	}
	return cloned
}

func (c *ConsistentHash) transferDataKeys(ctx context.Context, plan migrationPlan) error {
	if err := c.hashRing.DeleteNodeToDataKeys(ctx, plan.from, plan.dataSet); err != nil {
		return fmt.Errorf("remove migrated key index from %q: %w", plan.from, err)
	}
	if err := c.hashRing.AddNodeToDataKeys(ctx, plan.to, plan.dataSet); err != nil {
		rollbackErr := c.hashRing.AddNodeToDataKeys(ctx, plan.from, plan.dataSet)
		return errors.Join(fmt.Errorf("add migrated key index to %q: %w", plan.to, err), rollbackErr)
	}
	return nil
}

func (c *ConsistentHash) GetNode(ctx context.Context, dataKey string) (string, error) {
	if err := c.checkReady(ctx); err != nil {
		return "", err
	}
	if dataKey == "" {
		return "", errors.New("empty data key")
	}
	if err := c.hashRing.Lock(ctx, c.opts.lockExpireSeconds); err != nil {
		return "", err
	}

	defer func() {
		_ = c.hashRing.Unlock(ctx)
	}()

	dataScore, err := c.hashScore(dataKey)
	if err != nil {
		return "", err
	}
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

func (c *ConsistentHash) checkReady(ctx context.Context) error {
	if c == nil || c.hashRing == nil || c.encryptor == nil {
		return errors.New("consistent hash requires a ring and encryptor")
	}
	if ctx == nil {
		return errors.New("nil context")
	}
	return nil
}

func (c *ConsistentHash) hashScore(key string) (int32, error) {
	score := c.encryptor.Encrypt(key)
	if score < 0 || int64(score) >= int64(math.MaxInt32) {
		return 0, fmt.Errorf("hash score for %q is outside [0, MaxInt32): %d", key, score)
	}
	return score, nil
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

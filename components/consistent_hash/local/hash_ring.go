package local

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hangtiancheng/yukino.go/components/consistent_hash/pkg/os"
	"github.com/hangtiancheng/yukino.go/components/distributed_lock/utils"
)

type LockEntityV2 struct {
	locked   bool
	mutex    sync.Mutex
	expireAt time.Time
	owner    string
}

func NewLockEntityV2() *LockEntityV2 {
	return &LockEntityV2{}
}

func (l *LockEntityV2) Lock(ctx context.Context, expireSeconds int) error {
	if l == nil || ctx == nil {
		return errors.New("lock entity and context are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if expireSeconds < 0 || int64(expireSeconds) > int64(^uint64(0)>>1)/int64(time.Second) {
		return errors.New("lock expiration is out of range")
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()

	now := time.Now()
	if !l.locked || (!l.expireAt.IsZero() && !now.Before(l.expireAt)) {
		l.locked = true
		l.expireAt = time.Time{}
		if expireSeconds > 0 {
			l.expireAt = now.Add(time.Duration(expireSeconds) * time.Second)
		}
		l.owner = utils.GetProcessAndGoroutineIDStr()
		return nil
	}

	return errors.New("acquire by others")
}

func (l *LockEntityV2) Unlock(ctx context.Context) error {
	if l == nil || ctx == nil {
		return errors.New("lock entity and context are required")
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if !l.locked || (!l.expireAt.IsZero() && !time.Now().Before(l.expireAt)) {
		return errors.New("not locked")
	}

	if l.owner != utils.GetProcessAndGoroutineIDStr() {
		return errors.New("not your lock")
	}

	l.locked = false
	return nil
}

type SkiplistHashRing struct {
	LockEntity
	mu             sync.RWMutex
	root           *virtualNode
	nodeToReplicas map[string]int
	nodeToDataKey  map[string]map[string]struct{}
}

type LockEntity struct {
	lock       chan struct{}
	doubleLock sync.Mutex
	cancel     context.CancelFunc
	owner      atomic.Value
}

func NewSkiplistHashRing() *SkiplistHashRing {
	return &SkiplistHashRing{
		LockEntity:     LockEntity{lock: make(chan struct{}, 1)},
		root:           &virtualNode{},
		nodeToReplicas: make(map[string]int),
		nodeToDataKey:  make(map[string]map[string]struct{}),
	}
}

type virtualNode struct {
	score   int32
	nodeIDs []string
	nexts   []*virtualNode
}

func (s *SkiplistHashRing) Lock(ctx context.Context, expireSeconds int) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if expireSeconds < 0 || int64(expireSeconds) > int64(^uint64(0)>>1)/int64(time.Second) {
		return errors.New("lock expiration is out of range")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.lock <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-s.lock
		return err
	}
	s.doubleLock.Lock()
	token := os.GetCurrentProcessAndGoroutineIDStr()
	s.owner.Store(token)
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = nil
	if expireSeconds > 0 {
		ctx2, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		timer := time.NewTimer(time.Duration(expireSeconds) * time.Second)
		go func() {
			defer timer.Stop()
			select {
			case <-ctx2.Done():
			case <-timer.C:
				_ = s.unlock(ctx, token)
			}
		}()
	}
	s.doubleLock.Unlock()
	return nil
}

func (s *SkiplistHashRing) unlock(ctx context.Context, token string) error {
	s.doubleLock.Lock()
	defer s.doubleLock.Unlock()
	owner, _ := s.owner.Load().(string)
	if owner != token {
		return errors.New("not your lock")
	}
	s.owner.Store("")

	if s.cancel != nil {
		s.cancel()
	}

	select {
	case <-s.lock:
	default:
		return errors.New("lock is not held")
	}
	return nil
}

func (s *SkiplistHashRing) Unlock(ctx context.Context) error {
	token := os.GetCurrentProcessAndGoroutineIDStr()
	return s.unlock(ctx, token)
}

func (s *SkiplistHashRing) Add(ctx context.Context, score int32, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetNode, ok := s.get(score)
	if ok {
		if slices.Contains(targetNode.nodeIDs, nodeID) {
			return nil
		}
		targetNode.nodeIDs = append(targetNode.nodeIDs, nodeID)
		return nil
	}

	rLevel := s.roll()
	if len(s.root.nexts) < rLevel+1 {
		arr := make([]*virtualNode, rLevel+1-len(s.root.nexts))
		s.root.nexts = append(s.root.nexts, arr...)
	}

	newNode := virtualNode{
		score:   score,
		nexts:   make([]*virtualNode, rLevel+1),
		nodeIDs: []string{nodeID},
	}

	move := s.root
	for level := rLevel; level >= 0; level-- {
		for move.nexts[level] != nil && move.nexts[level].score < score {
			move = move.nexts[level]
		}
		newNode.nexts[level] = move.nexts[level]
		move.nexts[level] = &newNode
	}

	return nil
}

func (s *SkiplistHashRing) Ceiling(ctx context.Context, score int32) (int32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	target, ok := s.ceiling(score)
	if ok {
		return target, nil
	}

	first, _ := s.first()
	return first, nil
}

func (s *SkiplistHashRing) Floor(ctx context.Context, score int32) (int32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	target, ok := s.floor(score)
	if ok {
		return target, nil
	}

	last, _ := s.last()
	return last, nil
}

func (s *SkiplistHashRing) Rem(ctx context.Context, score int32, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	targetNode, ok := s.get(score)
	if !ok {
		return fmt.Errorf("score: %d not exist", score)
	}

	index := -1
	for i := 0; i < len(targetNode.nodeIDs); i++ {
		if targetNode.nodeIDs[i] == nodeID {
			index = i
			break
		}
	}

	if index == -1 {
		return fmt.Errorf("node: %s not exist in score: %d", nodeID, score)
	}

	if len(targetNode.nodeIDs) > 1 {
		targetNode.nodeIDs = append(targetNode.nodeIDs[:index], targetNode.nodeIDs[index+1:]...)
		return nil
	}

	move := s.root
	for level := range slices.Backward(s.root.nexts) {
		for move.nexts[level] != nil && move.nexts[level].score < score {
			move = move.nexts[level]
		}
		if move.nexts[level] == nil || move.nexts[level].score > score {
			continue
		}
		move.nexts[level] = move.nexts[level].nexts[level]
	}

	for level := 0; level < len(s.root.nexts); level++ {
		if s.root.nexts[level] != nil {
			continue
		}
		s.root.nexts = s.root.nexts[:level]
		break
	}
	return nil
}

func (s *SkiplistHashRing) Nodes(ctx context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodes := make(map[string]int, len(s.nodeToReplicas))
	for node, replicas := range s.nodeToReplicas {
		nodes[node] = replicas
	}
	return nodes, nil
}

func (s *SkiplistHashRing) AddNodeToReplica(ctx context.Context, nodeID string, replicas int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nodeToReplicas[nodeID] = replicas
	return nil
}

func (s *SkiplistHashRing) DeleteNodeToReplica(ctx context.Context, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.nodeToReplicas, nodeID)
	return nil
}

func (s *SkiplistHashRing) Node(ctx context.Context, score int32) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targetNode, ok := s.get(score)
	if !ok {
		return nil, fmt.Errorf("score: %d not exist", score)
	}
	nodeIDs := make([]string, len(targetNode.nodeIDs))
	copy(nodeIDs, targetNode.nodeIDs)
	return nodeIDs, nil
}

func (s *SkiplistHashRing) DataKeys(ctx context.Context, nodeID string) (map[string]struct{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dataKeys := s.nodeToDataKey[nodeID]
	if dataKeys == nil {
		return nil, nil
	}
	copied := make(map[string]struct{}, len(dataKeys))
	for dataKey := range dataKeys {
		copied[dataKey] = struct{}{}
	}
	return copied, nil
}

func (s *SkiplistHashRing) AddNodeToDataKeys(ctx context.Context, nodeID string, dataKeys map[string]struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldDataKeys := s.nodeToDataKey[nodeID]
	if oldDataKeys == nil {
		oldDataKeys = make(map[string]struct{})
	}
	for _dataKey := range dataKeys {
		oldDataKeys[_dataKey] = struct{}{}
	}
	s.nodeToDataKey[nodeID] = oldDataKeys
	return nil
}

func (s *SkiplistHashRing) DeleteNodeToDataKeys(ctx context.Context, nodeID string, dataKeys map[string]struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldDataKeys := s.nodeToDataKey[nodeID]
	if oldDataKeys == nil {
		return nil
	}
	for dataKey := range dataKeys {
		delete(oldDataKeys, dataKey)
	}
	if len(oldDataKeys) == 0 {
		delete(s.nodeToDataKey, nodeID)
	}
	return nil
}

func (s *SkiplistHashRing) roll() int {
	var level int
	for level < 31 && rand.Intn(2) == 1 {
		level++
	}
	return level
}

func (s *SkiplistHashRing) ceiling(score int32) (int32, bool) {
	if len(s.root.nexts) == 0 {
		return -1, false
	}

	move := s.root
	for level := range slices.Backward(s.root.nexts) {
		for move.nexts[level] != nil && move.nexts[level].score < score {
			move = move.nexts[level]
		}
	}

	if move.nexts[0] == nil {
		return -1, false
	}

	return move.nexts[0].score, true
}

func (s *SkiplistHashRing) first() (int32, bool) {
	if len(s.root.nexts) == 0 {
		return -1, false
	}

	return s.root.nexts[0].score, true
}

func (s *SkiplistHashRing) floor(score int32) (int32, bool) {
	if len(s.root.nexts) == 0 {
		return -1, false
	}

	move := s.root
	for level := range slices.Backward(s.root.nexts) {
		for move.nexts[level] != nil && move.nexts[level].score < score {
			move = move.nexts[level]
		}
	}

	if move.nexts[0] != nil && move.nexts[0].score == score {
		return score, true
	}

	if move == s.root {
		return -1, false
	}

	return move.score, true
}

func (s *SkiplistHashRing) last() (int32, bool) {
	move := s.root
	for level := range slices.Backward(s.root.nexts) {
		for move.nexts[level] != nil {
			move = move.nexts[level]
		}
	}

	if move == s.root {
		return -1, false
	}

	return move.score, true
}

func (s *SkiplistHashRing) get(score int32) (*virtualNode, bool) {
	move := s.root
	for level := range slices.Backward(s.root.nexts) {
		for move.nexts[level] != nil && move.nexts[level].score < score {
			move = move.nexts[level]
		}

		if move.nexts[level] != nil && move.nexts[level].score == score {
			return move.nexts[level], true
		}
	}

	return nil, false
}

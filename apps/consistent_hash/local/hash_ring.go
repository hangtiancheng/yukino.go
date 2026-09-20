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

	"github.com/hangtiancheng/yukino.go/apps/consistent_hash/pkg/os"
	"github.com/hangtiancheng/yukino.go/apps/redis_lock/utils"
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
	l.mutex.Lock()
	defer l.mutex.Unlock()

	now := time.Now()
	if !l.locked || l.expireAt.Before(now) {
		l.locked = true
		l.expireAt = now.Add(time.Duration(expireSeconds) * time.Second)
		l.owner = utils.GetProcessAndGoroutineIDStr()
		return nil
	}

	return errors.New("acquire by others")
}

func (l *LockEntityV2) Unlock(ctx context.Context) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if !l.locked || l.expireAt.Before(time.Now()) {
		return errors.New("not locked")
	}

	if l.owner != utils.GetProcessAndGoroutineIDStr() {
		return errors.New("not your lock")
	}

	l.locked = false
	return nil
}

// SkiplistHashRing is a local in-memory hash ring backed by a skip list.
type SkiplistHashRing struct {
	LockEntity
	// mu guards the ring state below. It is independent of the lease held via
	// Lock/Unlock, so the ring stays race-free even when the lease TTL expires
	// mid-operation and another goroutine starts mutating concurrently.
	mu   sync.RWMutex
	root *virtualNode
	// nodeToReplicas maps each node to its virtual-node count.
	nodeToReplicas map[string]int
	nodeToDataKey  map[string]map[string]struct{}
}

type LockEntity struct {
	lock sync.Mutex
	// doubleLock supports idempotent unlock.
	doubleLock sync.Mutex
	cancel     context.CancelFunc
	owner      atomic.Value
}

func NewSkiplistHashRing() *SkiplistHashRing {
	return &SkiplistHashRing{
		root:           &virtualNode{},
		nodeToReplicas: make(map[string]int),
		nodeToDataKey:  make(map[string]map[string]struct{}),
	}
}

type virtualNode struct {
	score int32
	// nodeIDs stores the node ids at this score.
	nodeIDs []string
	nexts   []*virtualNode
}

// Lock acquires the hash-ring lock with the given TTL. The lock auto-releases on expiry.
func (s *SkiplistHashRing) Lock(ctx context.Context, expireSeconds int) error {
	// Acquire the ring mutex before doubleLock: unlock (and the TTL watchdog)
	// needs doubleLock, so blocking on the ring mutex while holding doubleLock
	// would deadlock as soon as two goroutines contend for the lock.
	s.lock.Lock()
	s.doubleLock.Lock()
	defer s.doubleLock.Unlock()

	token := os.GetCurrentProcessAndGoroutineIDStr()
	s.owner.Store(token)
	if expireSeconds <= 0 {
		return nil
	}

	// Lock now, schedule unlock after the TTL. The unlock must verify ownership to avoid unlocking someone else's lock.
	ctx2, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		// This goroutine exits once an unlock is performed.
		select {
		case <-ctx2.Done():
			return
		case <-time.After(time.Duration(expireSeconds) * time.Second):
			s.unlock(ctx, token)
		}
	}()
	return nil
}

func (s *SkiplistHashRing) unlock(ctx context.Context, token string) error {
	// Unlock. If already unlocked, return immediately to avoid a fatal error.
	s.doubleLock.Lock()
	defer s.doubleLock.Unlock()
	// If the lock is not ours, bail out.
	owner, _ := s.owner.Load().(string)
	if owner != token {
		return errors.New("not your lock")
	}
	s.owner.Store("")

	// Our lock: cancel the watchdog goroutine.
	if s.cancel != nil {
		s.cancel()
	}

	s.lock.Unlock()
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

	// Top-down level traversal.
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

	delete(s.nodeToDataKey, nodeID)

	if len(targetNode.nodeIDs) > 1 {
		targetNode.nodeIDs = append(targetNode.nodeIDs[:index], targetNode.nodeIDs[index+1:]...)
		return nil
	}

	// Top-down level traversal to unlink the node.
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
	// Return a copy so callers can iterate it while the ring mutates.
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
	// Return a copy: the caller keeps reading the list while a concurrent Rem
	// may shift the original backing array.
	nodeIDs := make([]string, len(targetNode.nodeIDs))
	copy(nodeIDs, targetNode.nodeIDs)
	return nodeIDs, nil
}

func (s *SkiplistHashRing) DataKeys(ctx context.Context, nodeID string) (map[string]struct{}, error) {
	// Return a copy so callers can iterate it while the ring mutates.
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
	randInst := rand.New(rand.NewSource(time.Now().UnixNano()))
	var level int
	for randInst.Intn(2) == 1 {
		level++
	}
	return level
}

// ceiling returns the smallest score >= the given score.
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

// last returns the largest score in the ring.
func (s *SkiplistHashRing) last() (int32, bool) {
	// Top-down level traversal.
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

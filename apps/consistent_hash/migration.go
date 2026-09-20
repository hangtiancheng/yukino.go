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
	"math"
)

// Migrator is the user-supplied closure that performs data migration.
type Migrator func(ctx context.Context, dataKeys map[string]struct{}, from, to string) error

func (c *ConsistentHash) migrateIn(ctx context.Context, virtualScore int32, nodeID string) (from, to string, dataSet map[string]struct{}, _err error) {
	// No migrator injected: nothing to do.
	if c.migrator == nil {
		return
	}

	// Look up the nodes sharing this virtualScore. Multiple nodes can share one score.
	nodes, err := c.hashRing.Node(ctx, virtualScore)
	if err != nil {
		_err = err
		return
	}

	// If more than one node is at this score, the current node is not the first, so no migration is needed.
	if len(nodes) > 1 {
		return
	}

	// Find the previous score on the ring.
	lastScore, err := c.hashRing.Floor(ctx, c.decreaseScore(virtualScore))
	if err != nil {
		_err = err
		return
	}

	// No other score means the ring has only this node, so no migration is needed.
	if lastScore == -1 || lastScore == virtualScore {
		return
	}

	// Find the next score on the ring.
	nextScore, err := c.hashRing.Ceiling(ctx, c.incrScore(virtualScore))
	if err != nil {
		_err = err
		return
	}

	// No other score means the ring has only this node, so no migration is needed.
	if nextScore == -1 || nextScore == virtualScore {
		return
	}

	// patternOne: last-0-cur-next (the ring wraps around between last and cur).
	patternOne := lastScore > virtualScore
	// patternTwo: last-cur-0-next (the ring wraps around between cur and next).
	patternTwo := nextScore < virtualScore
	if patternOne {
		lastScore -= math.MaxInt32
	}

	if patternTwo {
		virtualScore -= math.MaxInt32
		lastScore -= math.MaxInt32
	}

	// Fetch the node at nextScore and read its data keys.
	nextNodes, err := c.hashRing.Node(ctx, nextScore)
	if err != nil {
		_err = err
		return
	}

	// Take the first node at nextScore and read its data keys.
	if len(nextNodes) == 0 {
		return
	}

	dataKeys, err := c.hashRing.DataKeys(ctx, c.getNodeID(nextNodes[0]))
	if err != nil {
		_err = err
		return
	}

	dataSet = make(map[string]struct{})
	// Iterate data keys and find those that fall into the new node's range.
	for dataKey := range dataKeys {
		dataVirtualScore := c.encryptor.Encrypt(dataKey)
		if patternOne && dataVirtualScore > (lastScore+math.MaxInt32) {
			dataVirtualScore -= math.MaxInt32
		}

		if patternTwo {
			dataVirtualScore -= math.MaxInt32
		}

		if dataVirtualScore <= lastScore || dataVirtualScore > virtualScore {
			continue
		}

		// This data key must migrate.
		dataSet[dataKey] = struct{}{}
	}

	if err = c.hashRing.DeleteNodeToDataKeys(ctx, c.getNodeID(nextNodes[0]), dataSet); err != nil {
		return "", "", nil, err
	}

	if err = c.hashRing.AddNodeToDataKeys(ctx, nodeID, dataSet); err != nil {
		return "", "", nil, err
	}

	// from, to, dataSet
	return c.getNodeID(nextNodes[0]), nodeID, dataSet, nil
}

func (c *ConsistentHash) migrateOut(ctx context.Context, virtualScore int32, nodeID string) (from, to string, dataSet map[string]struct{}, err error) {
	// No migrator injected: nothing to do.
	if c.migrator == nil {
		return
	}

	defer func() {
		if err != nil {
			return
		}
		if to == "" || len(dataSet) == 0 {
			return
		}

		if err = c.hashRing.DeleteNodeToDataKeys(ctx, nodeID, dataSet); err != nil {
			return
		}

		err = c.hashRing.AddNodeToDataKeys(ctx, to, dataSet)
	}()

	from = nodeID

	nodes, _err := c.hashRing.Node(ctx, virtualScore)
	if _err != nil {
		err = _err
		return
	}

	if len(nodes) == 0 {
		return
	}

	// Not the first node at this score: no migration needed.
	if c.getNodeID(nodes[0]) != nodeID {
		return
	}

	// No data: nothing to migrate.
	var allDataSet map[string]struct{}
	if allDataSet, err = c.hashRing.DataKeys(ctx, nodeID); err != nil {
		return
	}

	if len(allDataSet) == 0 {
		return
	}

	// Iterate data keys and find those in the range (lastScore, virtualScore].
	lastScore, _err := c.hashRing.Floor(ctx, c.decreaseScore(virtualScore))
	if _err != nil {
		err = _err
		return
	}

	var onlyScore bool
	if lastScore == -1 || lastScore == virtualScore {
		if len(nodes) == 1 {
			err = errors.New("no other no")
			return
		}
		onlyScore = true
	}

	pattern := lastScore > virtualScore
	if pattern {
		lastScore -= math.MaxInt32
	}

	dataSet = make(map[string]struct{})
	for data := range allDataSet {
		if onlyScore {
			dataSet[data] = struct{}{}
			continue
		}
		dataScore := c.encryptor.Encrypt(data)
		if pattern && dataScore > lastScore+math.MaxInt32 {
			dataScore -= math.MaxInt32
		}
		if dataScore <= lastScore || dataScore > virtualScore {
			continue
		}
		dataSet[data] = struct{}{}
	}

	// If multiple nodes share this score, delegate to the next node.
	if len(nodes) > 1 {
		to = c.getNodeID(nodes[1])
		return
	}

	// Find the successor node.
	if to, err = c.getValidNextNode(ctx, virtualScore, nodeID, nil); err != nil {
		// Keep the fresh error: _err is the stale (nil) Floor error, and
		// overwriting err with it would mask the real failure below.
		return
	}

	if to == "" {
		err = errors.New("no other node")
	}

	return
}

func (c *ConsistentHash) getValidNextNode(ctx context.Context, score int32, nodeID string, ranged map[int32]struct{}) (string, error) {
	// Find the successor score.
	nextScore, err := c.hashRing.Ceiling(ctx, c.incrScore(score))
	if err != nil {
		return "", err
	}
	if nextScore == -1 {
		return "", nil
	}

	if _, ok := ranged[nextScore]; ok {
		return "", nil
	}

	// The successor must not be the same node; otherwise keep searching.
	nextNodes, err := c.hashRing.Node(ctx, nextScore)
	if err != nil {
		return "", err
	}

	if len(nextNodes) == 0 {
		return "", errors.New("next node empty")
	}

	if nextNode := c.getNodeID(nextNodes[0]); nextNode != nodeID {
		return nextNode, nil
	}

	if len(nextNodes) > 1 {
		return c.getNodeID(nextNodes[1]), nil
	}

	if ranged == nil {
		ranged = make(map[int32]struct{})
	}
	ranged[score] = struct{}{}

	// Continue searching.
	return c.getValidNextNode(ctx, nextScore, nodeID, ranged)
}

func (c *ConsistentHash) incrScore(score int32) int32 {
	if score == math.MaxInt32-1 {
		return 0
	}
	return score + 1
}

func (c *ConsistentHash) decreaseScore(score int32) int32 {
	if score == 0 {
		return math.MaxInt32 - 1
	}
	return score - 1
}

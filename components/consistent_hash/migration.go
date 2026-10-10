package consistent_hash

import (
	"context"
	"errors"
	"math"
)

type Migrator func(ctx context.Context, dataKeys map[string]struct{}, from, to string) error

func (c *ConsistentHash) migrateIn(ctx context.Context, virtualScore int32, nodeID string) (from, to string, dataSet map[string]struct{}, _err error) {
	if c.migrator == nil {
		return
	}

	nodes, err := c.hashRing.Node(ctx, virtualScore)
	if err != nil {
		_err = err
		return
	}

	if len(nodes) > 1 {
		return
	}

	lastScore, err := c.hashRing.Floor(ctx, c.decreaseScore(virtualScore))
	if err != nil {
		_err = err
		return
	}

	if lastScore == -1 || lastScore == virtualScore {
		return
	}

	nextScore, err := c.hashRing.Ceiling(ctx, c.incrScore(virtualScore))
	if err != nil {
		_err = err
		return
	}

	if nextScore == -1 || nextScore == virtualScore {
		return
	}

	patternOne := lastScore > virtualScore
	patternTwo := nextScore < virtualScore
	if patternOne {
		lastScore -= math.MaxInt32
	}

	if patternTwo {
		virtualScore -= math.MaxInt32
		lastScore -= math.MaxInt32
	}

	nextNodes, err := c.hashRing.Node(ctx, nextScore)
	if err != nil {
		_err = err
		return
	}

	if len(nextNodes) == 0 {
		return
	}

	dataKeys, err := c.hashRing.DataKeys(ctx, c.getNodeID(nextNodes[0]))
	if err != nil {
		_err = err
		return
	}

	dataSet = make(map[string]struct{})
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

		dataSet[dataKey] = struct{}{}
	}

	if err = c.hashRing.DeleteNodeToDataKeys(ctx, c.getNodeID(nextNodes[0]), dataSet); err != nil {
		return "", "", nil, err
	}

	if err = c.hashRing.AddNodeToDataKeys(ctx, nodeID, dataSet); err != nil {
		return "", "", nil, err
	}

	return c.getNodeID(nextNodes[0]), nodeID, dataSet, nil
}

func (c *ConsistentHash) migrateOut(ctx context.Context, virtualScore int32, nodeID string) (from, to string, dataSet map[string]struct{}, err error) {
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

	if c.getNodeID(nodes[0]) != nodeID {
		return
	}

	var allDataSet map[string]struct{}
	if allDataSet, err = c.hashRing.DataKeys(ctx, nodeID); err != nil {
		return
	}

	if len(allDataSet) == 0 {
		return
	}

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

	if len(nodes) > 1 {
		to = c.getNodeID(nodes[1])
		return
	}

	if to, err = c.getValidNextNode(ctx, virtualScore, nodeID, nil); err != nil {
		return
	}

	if to == "" {
		err = errors.New("no other node")
	}

	return
}

func (c *ConsistentHash) getValidNextNode(ctx context.Context, score int32, nodeID string, ranged map[int32]struct{}) (string, error) {
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

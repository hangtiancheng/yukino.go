package model

import (
	"errors"
	"strconv"
	"sync"
	"time"
)

// Snowflake ID generator (standard-library implementation, replacing bwmarrin/snowflake).
// Layout: 1 sign bit + 41-bit millisecond timestamp + 10-bit node ID + 12-bit sequence number.
const (
	idEpoch int64 = 1577836800000 // 2020-01-01T00:00:00Z in milliseconds

	nodeBits     = 10
	sequenceBits = 12

	maxNode     = -1 ^ (-1 << nodeBits)     // 1023
	maxSequence = -1 ^ (-1 << sequenceBits) // 4095

	nodeShift      = sequenceBits            // 12
	timestampShift = nodeBits + sequenceBits // 22
)

var (
	idNode   *snowflakeNode
	idOnce   sync.Once
	idNodeMu sync.Mutex
)

type snowflakeNode struct {
	mu        sync.Mutex
	epoch     int64
	node      int64
	sequence  int64
	lastStamp int64
}

func newSnowflakeNode(node int64) (*snowflakeNode, error) {
	if node < 0 || node > maxNode {
		return nil, errors.New("node must be within 0-1023")
	}
	return &snowflakeNode{epoch: idEpoch, node: node}, nil
}

func (n *snowflakeNode) generate() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now().UnixMilli()
	if now == n.lastStamp {
		n.sequence = (n.sequence + 1) & maxSequence
		if n.sequence == 0 {
			// The sequence for the current millisecond is exhausted; spin until the next millisecond.
			for now <= n.lastStamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		n.sequence = 0
	}
	if now < n.lastStamp {
		// Clock rollback: keep issuing IDs with the last timestamp to avoid duplicate/negative IDs.
		now = n.lastStamp
	}
	n.lastStamp = now

	return (now-n.epoch)<<timestampShift | n.node<<nodeShift | n.sequence
}

// InitSnowflake initializes the snowflake ID generator.
// workerID should be unique across processes (0-1023).
func InitSnowflake(workerID int64) error {
	var initErr error
	idOnce.Do(func() {
		node, err := newSnowflakeNode(workerID)
		if err != nil {
			initErr = err
			return
		}
		idNodeMu.Lock()
		idNode = node
		idNodeMu.Unlock()
	})
	return initErr
}

// NextID generates the next int64 snowflake ID.
func NextID() int64 {
	idNodeMu.Lock()
	node := idNode
	idNodeMu.Unlock()
	if node == nil {
		_ = InitSnowflake(1)
		idNodeMu.Lock()
		node = idNode
		idNodeMu.Unlock()
	}
	return node.generate()
}

// NextIDString generates the decimal string representation of the next snowflake ID.
func NextIDString() string {
	return strconv.FormatInt(NextID(), 10)
}

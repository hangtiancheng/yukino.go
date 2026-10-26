package model

import (
	"errors"
	"strconv"
	"sync"
	"time"
)

// 雪花 ID 生成器（标准库实现，替代 bwmarrin/snowflake）。
// 结构：1 bit 符号位 + 41 bit 毫秒时间戳 + 10 bit 节点 ID + 12 bit 序列号。
const (
	idEpoch int64 = 1577836800000 // 2020-01-01T00:00:00Z 毫秒时间戳

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
		return nil, errors.New("node 必须在 0-1023 之间")
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
			// 当前毫秒序列号用尽，自旋等待下一毫秒。
			for now <= n.lastStamp {
				now = time.Now().UnixMilli()
			}
		}
	} else {
		n.sequence = 0
	}
	if now < n.lastStamp {
		// 时钟回拨：沿用上次时间戳继续发号，避免生成重复/负值 ID。
		now = n.lastStamp
	}
	n.lastStamp = now

	return (now-n.epoch)<<timestampShift | n.node<<nodeShift | n.sequence
}

// InitSnowflake 初始化雪花 ID 生成器。
// workerID 应在进程间唯一（0-1023）。
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

// NextID 生成下一个 int64 雪花 ID。
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

// NextIDString 生成下一个雪花 ID 的十进制字符串表示。
func NextIDString() string {
	return strconv.FormatInt(NextID(), 10)
}

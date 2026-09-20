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

package raft

type ReadState struct {
	// Commit index captured when the read request was received
	Index uint64
	// Unique identifier for the read request
	RequestCtx []byte
}

type readIndexStatus struct {
	req Message
	// Commit index at the time the read request was received
	index uint64
	// Set of nodes that acknowledged this read request
	acks map[uint64]struct{}
}

type readOnly struct {
	// Pending read requests keyed by request ID
	pendingReadIndex map[string]*readIndexStatus
	// Ordered queue of read request IDs
	readIndexQueue []string
}

func newReadOnly() *readOnly {
	return &readOnly{
		pendingReadIndex: make(map[string]*readIndexStatus),
	}
}

// addRequest tracks a read-index request keyed by its context. readIndex is
// the commit index captured when the leader received the request. It reports
// whether the request was newly added (duplicate contexts are ignored).
func (ro *readOnly) addRequest(ctx string, readIndex uint64, m Message) bool {
	if _, ok := ro.pendingReadIndex[ctx]; ok {
		return false
	}

	ro.pendingReadIndex[ctx] = &readIndexStatus{
		req:   m,
		index: readIndex,
		acks:  make(map[uint64]struct{}),
	}
	ro.readIndexQueue = append(ro.readIndexQueue, ctx)
	return true
}

// recvAck records a heartbeat acknowledgment for the pending read request
// identified by ctx from the given node, and returns the ack set collected so
// far.
func (ro *readOnly) recvAck(ctx string, from uint64) map[uint64]struct{} {
	rs, ok := ro.pendingReadIndex[ctx]
	if !ok {
		return nil
	}

	rs.acks[from] = struct{}{}
	return rs.acks
}

// advance completes the read requests queued before and including the one
// identified by ctx and returns their statuses in queue order.
func (ro *readOnly) advance(ctx string) []*readIndexStatus {
	var (
		i     int
		found bool
	)
	rss := make([]*readIndexStatus, 0, len(ro.readIndexQueue))
	for _, queued := range ro.readIndexQueue {
		i++
		rs, ok := ro.pendingReadIndex[queued]
		if !ok {
			panic("cannot find corresponding read state from pending map")
		}

		rss = append(rss, rs)
		if queued == ctx {
			found = true
			break
		}
	}

	if found {
		ro.readIndexQueue = ro.readIndexQueue[i:]
		for _, rs := range rss {
			delete(ro.pendingReadIndex, string(rs.req.Entries[0].Data))
		}
		return rss
	}
	return nil
}

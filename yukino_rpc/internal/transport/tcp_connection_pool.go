package transport

import (
	"context"
	"errors"
	"sync"
)

var ErrPoolClosed = errors.New("connection pool closed")

type ConnectionPool struct {
	addr string

	maxActive int

	conns []*TCPClient
	mu    sync.Mutex

	closed bool
	next   int
}

func NewConnectionPool(addr string, maxIdle, maxActive int) *ConnectionPool {
	return &ConnectionPool{
		addr:      addr,
		maxActive: maxActive,
		conns:     make([]*TCPClient, 0, maxActive),
	}
}

func (p *ConnectionPool) Acquire(ctx context.Context) (*TCPClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, ErrPoolClosed
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(p.conns) < p.maxActive {
		conn, err := newTCPClient(p.addr)
		if err != nil {
			return nil, err
		}
		p.conns = append(p.conns, conn)
		return conn, nil
	}

	n := len(p.conns)
	for i := 0; i < n; i++ {
		idx := (p.next + i) % len(p.conns)
		conn := p.conns[idx]

		if conn.closed.Load() == 0 {
			p.next = (idx + 1) % len(p.conns)
			return conn, nil
		}

		p.conns = append(p.conns[:idx], p.conns[idx+1:]...)
		if len(p.conns) == 0 {
			break
		}
		n--
		i--
	}

	conn, err := newTCPClient(p.addr)
	if err != nil {
		return nil, err
	}

	p.conns = append(p.conns, conn)
	return conn, nil
}

func (p *ConnectionPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}
	p.closed = true

	for _, conn := range p.conns {
		conn.Close()
	}
}

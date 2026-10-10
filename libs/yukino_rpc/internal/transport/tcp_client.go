package transport

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/protocol"
)

type TCPClient struct {
	conn *TCPConnection
	addr string

	writeMu sync.Mutex
	seq     atomic.Uint64

	pending sync.Map
	streams sync.Map

	closed atomic.Int32
}

func newTCPClient(addr string) (*TCPClient, error) {
	rawConn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}

	c := &TCPClient{
		conn: NewTCPConnection(rawConn),
		addr: addr,
	}

	go c.readLoop()
	return c, nil
}

func (c *TCPClient) nextSeq() uint64 {
	return c.seq.Add(1)
}

func (c *TCPClient) SendAsync(msg *protocol.Message) (*Future, error) {
	return c.SendAsyncWithCodec(msg, nil)
}

func (c *TCPClient) SendAsyncWithCodec(msg *protocol.Message, cc codec.Codec) (*Future, error) {
	if c.closed.Load() == 1 {
		return nil, errors.New("connection closed")
	}

	seq := c.nextSeq()
	msg.Header.RequestID = seq

	future := NewFutureWithCodec(cc)
	c.pending.Store(seq, future)

	if c.closed.Load() == 1 {
		if _, ok := c.pending.LoadAndDelete(seq); ok {
			return nil, errors.New("connection closed")
		}
	}

	c.writeMu.Lock()
	err := c.conn.Write(msg)
	c.writeMu.Unlock()

	if err != nil {
		c.pending.Delete(seq)
		c.fail(err)
		return nil, err
	}

	return future, nil
}

func (c *TCPClient) readLoop() {
	for {
		msg, err := c.conn.Read()
		if err != nil {
			c.fail(err)
			return
		}

		seq := msg.Header.RequestID

		switch msg.Header.StreamFlag {
		case protocol.StreamData:
			if val, ok := c.streams.Load(seq); ok {
				val.(*ClientStreamConn).Push(msg.Body)
			}
		case protocol.StreamEnd:
			if val, ok := c.streams.LoadAndDelete(seq); ok {
				val.(*ClientStreamConn).End()
			}
		case protocol.StreamError:
			if val, ok := c.streams.LoadAndDelete(seq); ok {
				val.(*ClientStreamConn).Error(errors.New(msg.Header.Error))
			}
		default:
			val, ok := c.pending.LoadAndDelete(seq)
			if !ok {
				continue
			}
			future := val.(*Future)
			if msg.Header.Error != "" {
				future.Done(nil, errors.New(msg.Header.Error))
			} else {
				future.Done(msg.Body, nil)
			}
		}
	}
}

func (c *TCPClient) SendStream(ctx context.Context, msg *protocol.Message, cc codec.Codec) (*ClientStreamConn, error) {
	if c.closed.Load() == 1 {
		return nil, errors.New("connection closed")
	}

	seq := c.nextSeq()
	msg.Header.RequestID = seq

	stream := NewClientStreamConn(ctx, cc)
	c.streams.Store(seq, stream)

	if c.closed.Load() == 1 {
		if _, ok := c.streams.LoadAndDelete(seq); ok {
			return nil, errors.New("connection closed")
		}
	}

	c.writeMu.Lock()
	err := c.conn.Write(msg)
	c.writeMu.Unlock()

	if err != nil {
		c.streams.Delete(seq)
		c.fail(err)
		return nil, err
	}

	return stream, nil
}

func (c *TCPClient) fail(err error) {
	c.shutdown(err)
}

func (c *TCPClient) shutdown(err error) {
	if !c.closed.CompareAndSwap(0, 1) {
		return
	}

	_ = c.conn.Close()

	c.pending.Range(func(key, value any) bool {
		future := value.(*Future)
		future.Done(nil, err)
		c.pending.Delete(key)
		return true
	})

	c.streams.Range(func(key, value any) bool {
		value.(*ClientStreamConn).Error(err)
		c.streams.Delete(key)
		return true
	})
}

func (c *TCPClient) Close() error {
	c.shutdown(errors.New("connection closed"))
	return nil
}

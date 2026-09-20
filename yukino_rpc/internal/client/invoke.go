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

package client

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/breaker"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/protocol"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/stream"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/transport"
)

func (c *Client) InvokeAsync(ctx context.Context, service string, method string, args any) (*transport.Future, error) {
	future, err := c.invokeAsync(ctx, service, method, args)
	if err != nil {
		return nil, err
	}
	go func() {
		timer := time.NewTimer(c.timeout)
		defer timer.Stop()
		select {
		case <-future.DoneChan():
		case <-timer.C:
			// Resolving the future here also fires OnComplete, so the
			// timeout is recorded on the circuit breaker exactly once.
			future.Done(nil, context.DeadlineExceeded)
		}
	}()
	return future, nil
}

// Invoke runs one RPC call and applies the client timeout to both send and wait.
func (c *Client) Invoke(ctx context.Context, service string, method string, args any, reply any) error {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	future, err := c.invokeAsync(callCtx, service, method, args)
	if err != nil {
		return err
	}
	err = future.GetResultWithContext(callCtx, reply)
	if err != nil && callCtx.Err() != nil {
		// Resolve the future on timeout/cancel so the breaker records the
		// failure; a late server response then becomes a no-op.
		future.Done(nil, callCtx.Err())
	}
	return err
}

func (c *Client) InvokeStream(ctx context.Context, service string, method string, args any) (stream.ClientStream, error) {
	if !c.limiter.Allow() {
		return nil, errors.New("rate limit exceeded")
	}

	addr, err := c.getAddr(service)
	if err != nil {
		return nil, err
	}
	br := c.getBreaker(service, addr)

	if !br.Allow() {
		return nil, errors.New("circuit breaker open")
	}

	acquireCtx, cancel := context.WithTimeout(ctx, c.timeout)
	conn, err := c.getPool(addr).Acquire(acquireCtx)
	cancel()
	if err != nil {
		br.RecordFailure()
		return nil, err
	}

	body, err := c.codec.Marshal(args)
	if err != nil {
		return nil, err
	}

	msg := &protocol.Message{
		Header: &protocol.Header{
			ServiceName: service,
			MethodName:  method,
			CodecType:   protocol.CodecType(c.codecType),
			Compression: codec.CompressionGzip,
		},
		Body: body,
	}

	s, err := conn.SendStream(ctx, msg, c.codec)
	if err != nil {
		br.RecordFailure()
		return nil, err
	}
	return &observedStream{inner: s, br: br}, nil
}

// observedStream feeds the stream outcome back into the circuit breaker:
// a clean EOF counts as success, any other terminal error as failure.
type observedStream struct {
	inner stream.ClientStream
	br    *breaker.CircuitBreaker
	once  sync.Once
}

func (o *observedStream) Recv(msg any) error {
	err := o.inner.Recv(msg)
	switch {
	case err == nil:
	case errors.Is(err, io.EOF):
		o.once.Do(o.br.RecordSuccess)
	case errors.Is(err, context.Canceled):
		// Caller-initiated cancellation is not a service failure.
	default:
		o.once.Do(o.br.RecordFailure)
	}
	return err
}

func (o *observedStream) Context() context.Context {
	return o.inner.Context()
}

func (c *Client) invokeAsync(ctx context.Context, service string, method string, args any) (*transport.Future, error) {
	if !c.limiter.Allow() {
		return nil, errors.New("rate limit exceeded")
	}

	addr, err := c.getAddr(service)
	if err != nil {
		return nil, err
	}
	br := c.getBreaker(service, addr)

	if !br.Allow() {
		return nil, errors.New("circuit breaker open")
	}

	acquireCtx, cancel := context.WithTimeout(ctx, c.timeout)
	conn, err := c.getPool(addr).Acquire(acquireCtx)
	cancel()
	if err != nil {
		br.RecordFailure()
		return nil, err
	}

	body, err := c.codec.Marshal(args)
	if err != nil {
		return nil, err
	}

	req := &protocol.Message{
		Header: &protocol.Header{
			ServiceName: service,
			MethodName:  method,
			CodecType:   protocol.CodecType(c.codecType),
			Compression: codec.CompressionGzip,
		},
		Body: body,
	}
	future, err := conn.SendAsyncWithCodec(req, c.codec)
	if err != nil {
		br.RecordFailure()
		return nil, err
	}

	future.OnComplete(func(err error) {
		if err != nil {
			br.RecordFailure()
			return
		}
		br.RecordSuccess()
	})
	return future, nil
}

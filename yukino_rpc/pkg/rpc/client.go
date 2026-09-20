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

package rpc

import (
	"context"
	"time"

	internal_client "github.com/hangtiancheng/yukino.go/yukino_rpc/internal/client"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/load_balance"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/protocol"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/registry"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/transport"
)

type DialOption func(*dialOptions)

type dialOptions struct {
	timeout      time.Duration
	codecType    codec.Type
	registry     *registry.Registry
	loadBalancer load_balance.LoadBalancer
}

func WithTimeout(d time.Duration) DialOption {
	return func(o *dialOptions) { o.timeout = d }
}

func WithDialCodec(t CodecType) DialOption {
	return func(o *dialOptions) { o.codecType = t }
}

func WithRegistry(reg *Registry) DialOption {
	return func(o *dialOptions) { o.registry = reg }
}

func WithLoadBalancer(lb LoadBalancer) DialOption {
	return func(o *dialOptions) { o.loadBalancer = lb }
}

type connMode int

const (
	modeStatic connMode = iota
	modeRegistry
)

type ClientConn struct {
	mode      connMode
	pool      *transport.ConnectionPool
	regClient *internal_client.Client
	codec     codec.Codec
	codecType codec.Type
	timeout   time.Duration
}

func Dial(target string, opts ...DialOption) (*ClientConn, error) {
	o := &dialOptions{
		timeout:   5 * time.Second,
		codecType: codec.JSON,
	}
	for _, opt := range opts {
		opt(o)
	}

	cc, err := codec.New(o.codecType)
	if err != nil {
		return nil, err
	}

	if o.registry != nil {
		var clientOpts []internal_client.ClientOption
		clientOpts = append(clientOpts, internal_client.WithClientTimeout(o.timeout))
		clientOpts = append(clientOpts, internal_client.WithClientCodec(o.codecType))
		if o.loadBalancer != nil {
			clientOpts = append(clientOpts, internal_client.WithClientLoadBalancer(o.loadBalancer))
		}
		regClient, err := internal_client.NewClient(o.registry, clientOpts...)
		if err != nil {
			return nil, err
		}
		return &ClientConn{
			mode:      modeRegistry,
			regClient: regClient,
			codec:     cc,
			codecType: o.codecType,
			timeout:   o.timeout,
		}, nil
	}

	pool := transport.NewConnectionPool(target, 0, 1)
	return &ClientConn{
		mode:      modeStatic,
		pool:      pool,
		codec:     cc,
		codecType: o.codecType,
		timeout:   o.timeout,
	}, nil
}

func (cc *ClientConn) Invoke(ctx context.Context, service, method string, args, reply any) error {
	switch cc.mode {
	case modeRegistry:
		return cc.regClient.Invoke(ctx, service, method, args, reply)
	default:
		return cc.invokeStatic(ctx, service, method, args, reply)
	}
}

func (cc *ClientConn) invokeStatic(ctx context.Context, service, method string, args, reply any) error {
	ctx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	future, err := cc.sendAsyncStatic(ctx, service, method, args)
	if err != nil {
		return err
	}
	return future.GetResultWithContext(ctx, reply)
}

// InvokeAsync sends a request without waiting for the response. The returned
// Future resolves with the reply, an error, or context.DeadlineExceeded once
// the dial timeout elapses.
func (cc *ClientConn) InvokeAsync(ctx context.Context, service, method string, args any) (*Future, error) {
	switch cc.mode {
	case modeRegistry:
		return cc.regClient.InvokeAsync(ctx, service, method, args)
	default:
		return cc.invokeAsyncStatic(ctx, service, method, args)
	}
}

func (cc *ClientConn) invokeAsyncStatic(ctx context.Context, service, method string, args any) (*Future, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	future, err := cc.sendAsyncStatic(acquireCtx, service, method, args)
	cancel()
	if err != nil {
		return nil, err
	}
	go func() {
		timer := time.NewTimer(cc.timeout)
		defer timer.Stop()
		select {
		case <-future.DoneChan():
		case <-timer.C:
			future.Done(nil, context.DeadlineExceeded)
		}
	}()
	return future, nil
}

func (cc *ClientConn) sendAsyncStatic(ctx context.Context, service, method string, args any) (*Future, error) {
	conn, err := cc.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	body, err := cc.codec.Marshal(args)
	if err != nil {
		return nil, err
	}

	return conn.SendAsyncWithCodec(&protocol.Message{
		Header: &protocol.Header{
			ServiceName: service,
			MethodName:  method,
			CodecType:   protocol.CodecType(cc.codecType),
			Compression: codec.CompressionGzip,
		},
		Body: body,
	}, cc.codec)
}

func (cc *ClientConn) NewStream(ctx context.Context, service, method string, args any) (ClientStream, error) {
	switch cc.mode {
	case modeRegistry:
		return cc.regClient.InvokeStream(ctx, service, method, args)
	default:
		return cc.newStreamStatic(ctx, service, method, args)
	}
}

func (cc *ClientConn) newStreamStatic(ctx context.Context, service, method string, args any) (ClientStream, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	conn, err := cc.pool.Acquire(acquireCtx)
	cancel()
	if err != nil {
		return nil, err
	}

	body, err := cc.codec.Marshal(args)
	if err != nil {
		return nil, err
	}

	msg := &protocol.Message{
		Header: &protocol.Header{
			ServiceName: service,
			MethodName:  method,
			CodecType:   protocol.CodecType(cc.codecType),
			Compression: codec.CompressionGzip,
		},
		Body: body,
	}

	stream, err := conn.SendStream(ctx, msg, cc.codec)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (cc *ClientConn) Close() error {
	switch cc.mode {
	case modeRegistry:
		cc.regClient.Close()
	default:
		cc.pool.Close()
	}
	return nil
}

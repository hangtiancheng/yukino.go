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
	"net"

	internal_server "github.com/hangtiancheng/yukino.go/yukino_rpc/internal/server"
)

type ServerOption func(*serverOptions)

type serverOptions struct {
	inner []internal_server.ServerOption
}

func WithCodec(t CodecType) ServerOption {
	return func(o *serverOptions) {
		o.inner = append(o.inner, internal_server.WithServerCodec(t))
	}
}

type Server struct {
	inner *internal_server.Server
}

func NewServer(opts ...ServerOption) *Server {
	o := &serverOptions{}
	for _, opt := range opts {
		opt(o)
	}
	inner, err := internal_server.NewServer(o.inner...)
	if err != nil {
		panic("yukino_rpc: invalid server option: " + err.Error())
	}
	return &Server{inner: inner}
}

func (s *Server) Register(name string, service any) {
	s.inner.Register(name, service)
}

func (s *Server) Serve(lis net.Listener) error {
	return s.inner.Serve(lis)
}

func (s *Server) GracefulStop() {
	s.inner.GracefulStop()
}

func (s *Server) Stop() {
	s.inner.Stop()
}

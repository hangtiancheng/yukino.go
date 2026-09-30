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

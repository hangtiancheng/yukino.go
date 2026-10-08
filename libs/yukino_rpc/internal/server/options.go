package server

import "github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/codec"

type HandleOption func(*Handler) error

func WithHandlerCodec(t codec.Type) HandleOption {
	return func(c *Handler) error {
		cc, err := codec.New(t)
		if err != nil {
			return err
		}
		c.codec = cc
		return nil
	}
}

type ServerOption func(*Server) error

func WithServerCodec(t codec.Type) ServerOption {
	return func(s *Server) error {
		handler, err := NewHandler(nil, WithHandlerCodec(t))
		if err != nil {
			return err
		}
		s.handler = handler
		return nil
	}
}

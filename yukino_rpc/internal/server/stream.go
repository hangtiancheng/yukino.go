package server

import (
	"context"

	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/protocol"
	"github.com/hangtiancheng/yukino.go/yukino_rpc/internal/transport"
)

type serverStream struct {
	conn      *transport.TCPConnection
	requestID uint64
	codec     codec.Codec
	ctx       context.Context
}

func (s *serverStream) Send(msg any) error {
	body, err := s.codec.Marshal(msg)
	if err != nil {
		return err
	}
	resp := &protocol.Message{
		Header: &protocol.Header{
			RequestID:   s.requestID,
			StreamFlag:  protocol.StreamData,
			Compression: codec.CompressionGzip,
		},
		Body: body,
	}
	return s.conn.Write(resp)
}

func (s *serverStream) Context() context.Context {
	return s.ctx
}

func (s *serverStream) end() error {
	resp := &protocol.Message{
		Header: &protocol.Header{
			RequestID:   s.requestID,
			StreamFlag:  protocol.StreamEnd,
			Compression: codec.CompressionGzip,
		},
	}
	return s.conn.Write(resp)
}

func (s *serverStream) sendError(errMsg string) error {
	resp := &protocol.Message{
		Header: &protocol.Header{
			RequestID:   s.requestID,
			StreamFlag:  protocol.StreamError,
			Error:       errMsg,
			Compression: codec.CompressionGzip,
		},
	}
	return s.conn.Write(resp)
}

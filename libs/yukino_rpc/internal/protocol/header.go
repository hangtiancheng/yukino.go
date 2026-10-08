package protocol

import "github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/codec"

type CodecType byte

const (
	CodecTypeJSON CodecType = iota + 1
	CodecTypeProto
)

type StreamFlag byte

const (
	StreamNone  StreamFlag = 0
	StreamData  StreamFlag = 1
	StreamEnd   StreamFlag = 2
	StreamError StreamFlag = 3
)

type Header struct {
	RequestID   uint64
	ServiceName string
	MethodName  string
	Error       string
	CodecType   CodecType
	Compression codec.CompressionType
	StreamFlag  StreamFlag `json:",omitempty"`
}

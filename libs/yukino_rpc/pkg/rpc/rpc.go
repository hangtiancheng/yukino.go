package rpc

import (
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/codec"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/load_balance"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/registry"
	"github.com/hangtiancheng/yukino.go/libs/yukino_rpc/internal/transport"
)

type CodecType = codec.Type

type Registry = registry.Registry
type Instance = registry.Instance
type LoadBalancer = load_balance.LoadBalancer

type Future = transport.Future

var (
	CodecJSON  = codec.JSON
	CodecProto = codec.PROTO
)

func NewRegistry(endpoints []string) (*Registry, error) {
	return registry.NewRegistry(endpoints)
}

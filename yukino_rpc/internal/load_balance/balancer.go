package load_balance

import "github.com/hangtiancheng/yukino.go/yukino_rpc/internal/registry"

type LoadBalancer interface {
	Select([]registry.Instance) registry.Instance
}

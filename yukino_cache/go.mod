module github.com/hangtiancheng/yukino.go/yukino_cache

go 1.26.0

require (
	github.com/hangtiancheng/yukino.go/yukino_http v0.0.2
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	go.etcd.io/etcd/api/v3 v3.7.0 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.7.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.44.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260720211330-0afa2a65878a // indirect
)

require (
	go.etcd.io/etcd/client/v3 v3.7.0
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260720211330-0afa2a65878a // indirect
)

replace (
	// github.com/hangtiancheng/yukino.go/yukino_cache => ../yukino_cache
	github.com/hangtiancheng/yukino.go/yukino_http => ../yukino_http
	github.com/hangtiancheng/yukino.go/yukino_orm => ../yukino_orm
	github.com/hangtiancheng/yukino.go/yukino_rpc => ../yukino_rpc
)

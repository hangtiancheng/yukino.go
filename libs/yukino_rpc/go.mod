module github.com/hangtiancheng/yukino.go/libs/yukino_rpc

go 1.26.0

require (
	go.etcd.io/etcd/client/v3 v3.7.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	go.etcd.io/etcd/api/v3 v3.7.0 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.7.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.47.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/grpc v1.83.2 // indirect
)

replace (
	github.com/hangtiancheng/yukino.go/libs/yukino_cache => ../yukino_cache
	github.com/hangtiancheng/yukino.go/libs/yukino_http => ../yukino_http
	github.com/hangtiancheng/yukino.go/libs/yukino_orm => ../yukino_orm
// github.com/hangtiancheng/yukino.go/libs/yukino_rpc => ../yukino_rpc
)

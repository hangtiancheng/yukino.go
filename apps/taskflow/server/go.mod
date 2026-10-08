module github.com/hangtiancheng/yukino.go/apps/taskflow/server

go 1.26.4

require (
	github.com/getsentry/sentry-go v0.30.0
	github.com/hangtiancheng/yukino.go/components/consistent_cache v0.0.0
	github.com/hangtiancheng/yukino.go/components/consistent_hash v0.0.0
	github.com/hangtiancheng/yukino.go/components/lsm_tree v0.0.0
	github.com/hangtiancheng/yukino.go/components/raft v0.0.0
	github.com/hangtiancheng/yukino.go/components/red_mq v0.0.0
	github.com/hangtiancheng/yukino.go/components/redis_lock v0.0.0
	github.com/hangtiancheng/yukino.go/components/tcc v0.0.0
	github.com/hangtiancheng/yukino.go/components/time_wheel v0.0.0
	github.com/hangtiancheng/yukino.go/components/timer v0.0.0
	github.com/hangtiancheng/yukino.go/libs/yukino_cache v0.0.0
	github.com/hangtiancheng/yukino.go/libs/yukino_http v0.0.1
	github.com/openai/openai-go v1.12.0
	github.com/redis/go-redis/v9 v9.22.0
	go.opentelemetry.io/otel v1.46.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.46.0
	go.opentelemetry.io/otel/sdk v1.46.0
	go.opentelemetry.io/otel/trace v1.46.0
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/driver/mysql v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/pkg/errors v0.9.2-0.20201214064552-5dd12d0cfe7f // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/twmb/murmur3 v1.2.0 // indirect
	go.etcd.io/etcd/api/v3 v3.7.0 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.7.0 // indirect
	go.etcd.io/etcd/client/v3 v3.7.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260720211330-0afa2a65878a // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260720211330-0afa2a65878a // indirect
	google.golang.org/grpc v1.83.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/hangtiancheng/yukino.go/components/consistent_cache => ../../../components/consistent_cache
	github.com/hangtiancheng/yukino.go/components/consistent_hash => ../../../components/consistent_hash
	github.com/hangtiancheng/yukino.go/components/lsm_tree => ../../../components/lsm_tree
	github.com/hangtiancheng/yukino.go/components/raft => ../../../components/raft
	github.com/hangtiancheng/yukino.go/components/red_mq => ../../../components/red_mq
	github.com/hangtiancheng/yukino.go/components/redis_lock => ../../../components/redis_lock
	github.com/hangtiancheng/yukino.go/components/tcc => ../../../components/tcc
	github.com/hangtiancheng/yukino.go/components/time_wheel => ../../../components/time_wheel
	github.com/hangtiancheng/yukino.go/components/timer => ../../../components/timer
	github.com/hangtiancheng/yukino.go/libs/yukino_cache => ../../../libs/yukino_cache
	github.com/hangtiancheng/yukino.go/libs/yukino_http => ../../../libs/yukino_http
)

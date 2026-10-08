module github.com/hangtiancheng/yukino.go/components/consistent_hash

go 1.26

require (
	github.com/hangtiancheng/yukino.go/components/redis_lock v0.0.0
	github.com/redis/go-redis/v9 v9.22.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

replace github.com/hangtiancheng/yukino.go/components/redis_lock => ../redis_lock

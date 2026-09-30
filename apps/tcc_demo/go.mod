module github.com/hangtiancheng/yukino.go/apps/tcc_demo

go 1.26.0

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/google/uuid v1.6.0
	github.com/hangtiancheng/yukino.go/apps/redis_lock v0.0.0
	github.com/redis/go-redis/v9 v9.22.0
	go.uber.org/mock v0.6.0
	gorm.io/driver/mysql v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/hangtiancheng/yukino.go/apps/redis_lock => ../redis_lock

replace github.com/hangtiancheng/yukino.go/yukino_http => ../../yukino_http

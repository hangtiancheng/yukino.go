module github.com/hangtiancheng/yukino.go/apps/takeout-agent

go 1.26.0

require (
	github.com/agentscope-ai/agentscope-go/v2 v2.0.11
	github.com/google/uuid v1.6.0
	github.com/hangtiancheng/yukino.go/libs/yukino_http v0.0.1
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/driver/mysql v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace (
	github.com/hangtiancheng/yukino.go/libs/yukino_cache => ../../libs/yukino_cache
	github.com/hangtiancheng/yukino.go/libs/yukino_http => ../../libs/yukino_http
	github.com/hangtiancheng/yukino.go/libs/yukino_orm => ../../libs/yukino_orm
	github.com/hangtiancheng/yukino.go/libs/yukino_rpc => ../../libs/yukino_rpc
)

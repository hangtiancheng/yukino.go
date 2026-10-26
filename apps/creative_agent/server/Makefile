.PHONY: build run test test-race clean lint test-integration ping

APP_NAME = creative-agent
BUILD_DIR = build

build:
	CGO_ENABLED=1 go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/api

run:
	CGO_ENABLED=1 go run ./cmd/api

# sqlite 测试驱动（gorm.io/driver/sqlite）依赖 cgo，测试必须 CGO_ENABLED=1。
test:
	CGO_ENABLED=1 go test ./... -count=1

test-race:
	CGO_ENABLED=1 go test ./... -race -count=1

clean:
	rm -rf $(BUILD_DIR)

lint:
	golangci-lint run ./...

test-integration:
	CGO_ENABLED=1 go test -tags=integration ./...

# 连通性探测：验证远程 MySQL / Redis 可达且凭据正确（复用 config + secrets.json）。
# 可用 REDIS_DB=15 之类的环境变量覆盖 Redis DB，避免污染远程 DB0 缓存。
ping:
	CGO_ENABLED=1 go test -tags=integration ./internal/app/ -run TestConnectivity -v

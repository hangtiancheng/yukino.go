.PHONY: build run test test-race clean lint test-integration ping

APP_NAME = creative-agent
BUILD_DIR = build

build:
	CGO_ENABLED=1 go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/api

run:
	CGO_ENABLED=1 go run ./cmd/api

# The sqlite test driver (gorm.io/driver/sqlite) requires cgo; tests must run with CGO_ENABLED=1.
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

# Connectivity probe: verifies remote MySQL / Redis are reachable with correct credentials (reusing config + secrets.json).
# Override the Redis DB with env vars like REDIS_DB=15 to avoid polluting the remote DB0 cache.
ping:
	CGO_ENABLED=1 go test -tags=integration ./internal/app/ -run TestConnectivity -v

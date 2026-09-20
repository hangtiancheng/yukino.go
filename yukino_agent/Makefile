# Yukino Agent Makefile
# Unified build / run / test entry points

GO ?= go
APP := yukino-agent
PKG := ./...

.PHONY: all build run dev test vet tidy fmt clean docker-up docker-down docker-build help

all: build

# Compile binary to bin/
build:
	$(GO) build -o bin/$(APP) .

# Run directly
run:
	$(GO) run .

# Hot-reload development with air
dev:
	air

# Run tests
test:
	$(GO) test $(PKG) -race -cover

# go vet
vet:
	$(GO) vet $(PKG)

# go mod tidy
tidy:
	$(GO) mod tidy

# Format code
fmt:
	$(GO) fmt $(PKG)

# Clean build artifacts
clean:
	rm -rf bin/ tmp/

# Start infrastructure (Redis/Prometheus/Grafana)
docker-up:
	docker compose up -d

# Stop infrastructure
docker-down:
	docker compose down

# Build Docker image
docker-build:
	docker build -t $(APP) .

help:
	@echo "Yukino Agent Makefile targets:"
	@echo "  make build        Compile to bin/$(APP)"
	@echo "  make run          go run ."
	@echo "  make dev          Hot-reload with air"
	@echo "  make test         Run tests"
	@echo "  make vet          go vet"
	@echo "  make tidy         go mod tidy"
	@echo "  make fmt          go fmt"
	@echo "  make clean        Clean bin/ tmp/"
	@echo "  make docker-up    Start Redis/Prometheus/Grafana"
	@echo "  make docker-down  Stop infrastructure"
	@echo "  make docker-build Build Docker image"

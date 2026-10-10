
GO ?= go
APP := yukino-agent
PKG := ./...

.PHONY: all build run dev test vet tidy fmt clean docker-up docker-down docker-build help

all: build

build:
	$(GO) build -o bin/$(APP) ./cmd/agent

run:
	$(GO) run ./cmd/agent

dev:
	air

test:
	$(GO) test $(PKG) -race -cover

vet:
	$(GO) vet $(PKG)

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt $(PKG)

clean:
	rm -rf bin/ tmp/

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-build:
	docker build -f Dockerfile -t $(APP) ../../..

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
	@echo "  make docker-up    Start Milvus/Prometheus/Grafana"
	@echo "  make docker-down  Stop infrastructure"
	@echo "  make docker-build Build Docker image"

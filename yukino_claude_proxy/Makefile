.PHONY: build install test race vet

build:
	go build -o bin/yukino-claude-proxy ./cmd/yukino-claude-proxy

install:
	go install ./cmd/yukino-claude-proxy

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

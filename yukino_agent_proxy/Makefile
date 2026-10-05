.PHONY: build build-all install test race vet release


build:
	go build -o bin/yukino-agent-proxy ./cmd/yukino-agent-proxy

build-all:
	node build.mjs

install:
	go install ./cmd/yukino-agent-proxy

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

release:
	node ../release.js yukino-agent-proxy

.PHONY: build build-all install test race vet release


build:
	go build -o bin/yukino-agent-proxy ./cmd/agent_proxy

build-all:
	node build.mjs

install:
	go install ./cmd/agent_proxy

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

release:
	node ../../scripts/release.js yukino-agent-proxy

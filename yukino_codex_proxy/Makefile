.PHONY: build build-all install test race vet release

build:
	go build -trimpath -o bin/yukino-codex-proxy ./cmd/yukino-codex-proxy

build-all:
	node build.mjs

install:
	go install ./cmd/yukino-codex-proxy

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

release:
	node ../release.js yukino-codex-proxy

.PHONY: all build test race vet fmt clean run-server run-agent run-backup

BIN := bin

all: build

build:
	go build -o $(BIN)/ ./cmd/...

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

run-server: build
	./$(BIN)/storaged -config configs/storage.json -log-config configs/log.json

run-agent: build
	./$(BIN)/storage-agent -config configs/agent.json -log-config configs/log.json

run-backup: build
	./$(BIN)/logbackupd -addr 0.0.0.0:8080 -out ./logfile/backup.log

clean:
	rm -rf $(BIN)
	rm -rf ./deep_storage ./low_storage ./logfile ./temp
	rm -f ./storage.data ./storage.dat

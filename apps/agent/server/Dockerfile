
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY libs ./libs
COPY apps/agent/server ./apps/agent/server

WORKDIR /src/apps/agent/server

RUN CGO_ENABLED=0 GOOS=linux GOWORK=off go build -o /out/yukino-agent ./cmd/agent

FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/yukino-agent /app/yukino-agent

COPY apps/agent/server/config.example.jsonc /app/config.example.jsonc

EXPOSE 8123

ENTRYPOINT ["/app/yukino-agent"]

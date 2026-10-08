# Yukino Agent multi-stage build.
#
# The build context MUST be the repository root, because go.mod replaces the
# yukino.go libraries with relative paths (../../../libs/*) that only resolve
# when the monorepo layout is present:
#
#   docker build -f apps/agent/server/Dockerfile -t yukino-agent .
#
# Run (config.json is not baked into the image — mount it):
#
#   docker run -p 8123:8123 \
#     -v $(pwd)/apps/agent/server/config.json:/app/config.json:ro \
#     yukino-agent

# ---- Build stage ----
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Local modules referenced by the replace directives, then the app itself.
COPY libs ./libs
COPY apps/agent/server ./apps/agent/server

WORKDIR /src/apps/agent/server

# GOWORK=off: the repository go.work lists modules that are not part of this
# image; the module builds standalone via its own go.mod + replace directives.
RUN CGO_ENABLED=0 GOOS=linux GOWORK=off go build -o /out/yukino-agent ./cmd/agent

# ---- Runtime stage ----
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /out/yukino-agent /app/yukino-agent

# Reference configuration only. The real config.json (which holds API keys) is
# mounted at runtime; without it the process exits with a clear load error.
COPY apps/agent/server/config.example.jsonc /app/config.example.jsonc

EXPOSE 8123

ENTRYPOINT ["/app/yukino-agent"]

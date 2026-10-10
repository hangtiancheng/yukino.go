# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY components/consistent_cache/ ./components/consistent_cache/
COPY components/consistent_hash/ ./components/consistent_hash/
COPY components/lsm_tree/ ./components/lsm_tree/
COPY components/raft/ ./components/raft/
COPY components/red_mq/ ./components/red_mq/
COPY components/redis_lock/ ./components/redis_lock/
COPY components/tcc/ ./components/tcc/
COPY components/time_wheel/ ./components/time_wheel/
COPY components/timer/ ./components/timer/
COPY libs/yukino_cache/ ./libs/yukino_cache/
COPY libs/yukino_http/ ./libs/yukino_http/

COPY services/taskflow/server/go.mod services/taskflow/server/go.sum ./services/taskflow/server/
WORKDIR /src/services/taskflow/server
ENV GOWORK=off CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY services/taskflow/server/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/taskflow ./cmd/taskflow

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S taskflow && adduser -S taskflow -G taskflow
WORKDIR /app
COPY --from=build /out/taskflow ./taskflow
COPY services/taskflow/server/conf.docker.yml ./conf.yml
RUN mkdir -p /app/reports /app/data /app/logs && chown -R taskflow:taskflow /app
USER taskflow
EXPOSE 8090
ENTRYPOINT ["./taskflow"]
CMD ["--config", "conf.yml"]

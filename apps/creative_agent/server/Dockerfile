# Stage 1: Build（生产二进制不依赖 sqlite/cgo，静态编译）
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server ./cmd/api

# Stage 2: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /server .
COPY config/ config/
# SKILL.md 动态工具源：不拷贝则容器内 skill 功能静默失效
COPY skills/ skills/
EXPOSE 8080
CMD ["./server"]

# Stage 1: Build (the production binary does not depend on sqlite/cgo; static compilation)
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
# SKILL.md dynamic tool sources: if not copied, the skill feature silently stops working inside the container
COPY skills/ skills/
EXPOSE 8080
CMD ["./server"]

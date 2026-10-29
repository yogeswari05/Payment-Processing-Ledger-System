# Multi-stage Dockerfile for PayFlow
# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Compile static binaries for API and Worker
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /bin/payflow-api ./cmd/api

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /bin/payflow-worker ./cmd/worker

# Runtime stage
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata curl

COPY --from=builder /bin/payflow-api /app/payflow-api
COPY --from=builder /bin/payflow-worker /app/payflow-worker
COPY migrations /app/migrations

EXPOSE 8080

ENTRYPOINT ["/app/payflow-api"]

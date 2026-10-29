.PHONY: all build run worker test test-unit test-integration test-race bench docker-up docker-down clean

all: build test

build:
	go build -o bin/payflow-api ./cmd/api
	go build -o bin/payflow-worker ./cmd/worker

run:
	go run ./cmd/api

worker:
	go run ./cmd/worker

test: test-unit test-integration

test-unit:
	go test -v ./internal/domain/... ./internal/api/...

test-integration:
	go test -v ./internal/repository/postgres/...

test-race:
	go test -race -v ./internal/domain/... ./internal/api/...

bench:
	cd internal/repository/postgres && go test -bench=Benchmark -benchmem -benchtime=50x

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v

clean:
	rm -rf bin/ .pg_test*

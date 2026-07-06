.PHONY: run test build fmt tidy db-up db-down

run:
	go run ./cmd/server

test:
	go test ./... -v

build:
	go build -o bin/fraud-shield ./cmd/server

fmt:
	go fmt ./...

tidy:
	go mod tidy

db-up:
	docker compose up -d

db-down:
	docker compose down

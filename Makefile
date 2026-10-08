.PHONY: run test test-race cover build fmt vet tidy db-up db-down docker-build docker-run deploy-openshift teardown-openshift

IMAGE ?= fraud-shield:latest

run:
	go run ./cmd/server

test:
	go test ./... -v

test-race:
	go test -race -count=1 ./...

cover:
	go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out

build:
	go build -o bin/fraud-shield ./cmd/server

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy

db-up:
	docker compose up -d

db-down:
	docker compose down

docker-build:
	docker build -t $(IMAGE) .

docker-run: docker-build
	docker run --rm -p 8080:8080 $(IMAGE)

# Requires `oc login` and DB_PASSWORD in the environment.
deploy-openshift:
	cd ansible && ansible-playbook deploy.yml

teardown-openshift:
	cd ansible && ansible-playbook teardown.yml

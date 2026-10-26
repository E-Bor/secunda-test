.PHONY: init up down test integration vet build fmt

init:
	@sh scripts/init-env.sh

up:
	docker compose up --build -d --wait

down:
	docker compose --profile test down

test:
	go test -race ./...

integration:
	docker compose --profile test run --build --rm tests

vet:
	go vet ./...

build:
	go build -trimpath -o bin/secunda ./cmd/api

fmt:
	gofmt -w cmd internal

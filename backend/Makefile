.PHONY: run test vet build

run:
	go run ./cmd/api

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -o bin/api ./cmd/api

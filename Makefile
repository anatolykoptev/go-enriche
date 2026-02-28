.PHONY: lint test build

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 ./...

build:
	go build ./...

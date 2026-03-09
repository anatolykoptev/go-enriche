.PHONY: lint test build

lint:
	GOWORK=off golangci-lint run ./...

test:
	GOWORK=off go test -race -count=1 ./...

build:
	GOWORK=off go build ./...

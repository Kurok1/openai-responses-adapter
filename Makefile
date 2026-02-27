APP := adapter

.PHONY: fmt build test run

fmt:
	gofmt -w $(shell find . -name '*.go' -not -path './vendor/*')

build:
	go build ./...

test:
	go test ./...

run:
	go run ./cmd/adapter

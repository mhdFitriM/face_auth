.PHONY: build test check-functions

build:
	go build ./...

test:
	go test ./internal/... -v

check-functions:
	go run ./cmd/check-functions/

check-functions-build:
	go build -o ./bin/check-functions ./cmd/check-functions/

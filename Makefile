BINARY  := go-cs-metrics
MODULE  := github.com/pable/go-cs-metrics
GOFLAGS :=

.PHONY: all build test vet lint tidy clean install help

all: vet test build

## build: compile the binary into the repo root
build:
	go build $(GOFLAGS) -o $(BINARY) .

## test: run all unit tests
test:
	go test ./...

## test-v: run all unit tests with verbose output
test-v:
	go test -v ./...

## test-cover: run tests and open a coverage report
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

## vet: run go vet
vet:
	go vet ./...

## tidy: tidy and verify the module graph
tidy:
	go mod tidy
	go mod verify

## clean: remove the compiled binary and coverage output
clean:
	rm -f $(BINARY) coverage.out

## install: install the binary to $GOPATH/bin (or ~/go/bin)
install:
	go install $(GOFLAGS) .

## help: list available targets
help:
	@grep -E '^##' Makefile | sed 's/^## /  /'

BINARY_NAME=upuai
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-s -w \
	-X github.com/upuai-cloud/cli/pkg/version.Version=$(VERSION) \
	-X github.com/upuai-cloud/cli/pkg/version.Commit=$(COMMIT) \
	-X github.com/upuai-cloud/cli/pkg/version.BuildDate=$(BUILD_DATE)"

.PHONY: build clean install test lint fmt

build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) .

clean:
	rm -rf bin/

INSTALL_DIR ?= /usr/local/bin

install: build
	cp bin/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)

test:
	go test ./... -v -race

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w .

dev: build
	./bin/$(BINARY_NAME)

.DEFAULT_GOAL := build

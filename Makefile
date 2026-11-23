# Lapel build automation
# Works on Windows (Git Bash/WSL) and Linux/macOS

BINARY := lapel.exe
BUILD_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: all build test vet fmt clean doctor docs build-windows build-linux build-darwin build-all

all: build

## Build the binary
build:
	@echo "Building $(BINARY)..."
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/lapel/
	@echo "Built: $(BUILD_DIR)/$(BINARY)"

## Run unit tests
test:
	go test ./...

## Run go vet
vet:
	go vet ./...

## Check formatting
fmt:
	gofmt -w .

## Generate static HTML documentation from docs/*.md
docs:
	go run ./cmd/docsgen -docs-dir docs

## Clean local build artifacts (does not wipe the global Go cache)
clean:
	rm -rf $(BUILD_DIR)

## Build and run doctor command
doctor: build
	./$(BUILD_DIR)/$(BINARY) doctor

## Cross-compile for Windows
build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/lapel-windows-amd64.exe ./cmd/lapel/

## Cross-compile for Linux
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/lapel-linux-amd64 ./cmd/lapel/

## Cross-compile for macOS
build-darwin:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/lapel-darwin-arm64 ./cmd/lapel/

## Build for all platforms
build-all: build-windows build-linux build-darwin

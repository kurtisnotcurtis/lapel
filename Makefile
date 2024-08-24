# Lapel build automation
# Works on Windows (Git Bash/WSL) and Linux/macOS

BINARY := lapel.exe
BUILD_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go build flags for version info
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: all build test clean run doctor

all: build

## Build the binary
build:
	@echo "Building $(BINARY)..."
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/lapel/
	@echo "Built: $(BUILD_DIR)/$(BINARY)"

## Run tests
test:
	go test ./...

## Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	go clean -cache

## Build and run doctor command
doctor: build
	./$(BUILD_DIR)/$(BINARY) doctor

## Build and run the CLI (pass args after --)
run: build
	./$(BUILD_DIR)/$(BINARY) $(filter-out $@,$(MAKECMDGOALS))

## Cross-compile for Windows
build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/lapel.exe ./cmd/lapel/

## Cross-compile for Linux
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/lapel ./cmd/lapel/

## Cross-compile for macOS
build-darwin:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/lapel ./cmd/lapel/

## Build for all platforms
build-all: build-windows build-linux build-darwin

# Catch-all target for passing arguments to run
%:
	@:

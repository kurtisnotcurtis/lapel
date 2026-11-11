#!/bin/bash
# Lapel build script
# Works on Windows (WSL/Git Bash) and Linux/macOS

set -e

BINARY="lapel.exe"
BUILD_DIR="bin"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go build flags for version info
LDFLAGS=("-ldflags" "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}")

usage() {
    echo "Usage: $0 [command]"
    echo ""
    echo "Commands:"
    echo "  build       Build the binary (default)"
    echo "  test        Run tests"
    echo "  clean       Remove build artifacts"
    echo "  doctor      Build and run doctor command"
    echo "  run         Build and run CLI (pass args after --)"
    echo ""
    exit 1
}

build() {
    echo "Building ${BINARY}..."
    mkdir -p "${BUILD_DIR}"
    go build "${LDFLAGS[@]}" -o "${BUILD_DIR}/${BINARY}" ./cmd/lapel/
    echo "Built: ${BUILD_DIR}/${BINARY}"
}

test() {
    go test ./...
}

clean() {
    rm -rf "${BUILD_DIR}"
    go clean -cache
    echo "Cleaned."
}

doctor() {
    build
    "./${BUILD_DIR}/${BINARY}" doctor
}

run() {
    build
    "./${BUILD_DIR}/${BINARY}" "$@"
}

# Parse arguments
case "${1:-build}" in
    build)
        build
        ;;
    test)
        test
        ;;
    clean)
        clean
        ;;
    doctor)
        doctor
        ;;
    run)
        shift
        run "$@"
        ;;
    *)
        usage
        ;;
esac

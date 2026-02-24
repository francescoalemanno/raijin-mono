#!/bin/bash

# Build script for Raijin - builds for all supported architectures with CGO disabled

set +e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Output directory
OUTPUT_DIR="build"
mkdir -p "$OUTPUT_DIR"

# Disable CGO
export CGO_ENABLED=0

echo -e "${GREEN}Building Raijin for all architectures (CGO disabled)${NC}"
echo -e "${YELLOW}Output directory: $OUTPUT_DIR${NC}"
echo ""

# Get version info from git if available
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS="-X main.Version=$VERSION -X main.BuildTime=$BUILD_TIME -s -w"

# Build function
build_target() {
    local os=$1
    local arch=$2
    local ext=$3
    local output_name="raijin-${os}-${arch}${ext}"

    echo -e "${YELLOW}Building for $os/$arch${NC}"

    GOOS=$os GOARCH=$arch go build \
        -ldflags "$LDFLAGS" \
        -trimpath \
        -o "$OUTPUT_DIR/$output_name" \
        ./cmd/raijin

    # Calculate file size
    local size
    size=$(ls -lh "$OUTPUT_DIR/$output_name" | awk '{print $5}')

    # Check if binary exists and is executable
    if [ -f "$OUTPUT_DIR/$output_name" ]; then
        echo -e "${GREEN}✓ Built: $output_name ($size)${NC}"
    else
        echo -e "${RED}✗ Failed to build: $output_name${NC}"
        return 1
    fi

    return 0
}

# Counter for successful builds
SUCCESS_COUNT=0
FAILED_COUNT=0
TOTAL_COUNT=0

# Linux builds
build_target "linux" "amd64" "" || ((FAILED_COUNT++))
build_target "linux" "arm64" "" || ((FAILED_COUNT++))
build_target "linux" "386" "" || ((FAILED_COUNT++))
build_target "linux" "arm" "" || ((FAILED_COUNT++))
build_target "linux" "riscv64" "" || ((FAILED_COUNT++))
build_target "linux" "ppc64le" "" || ((FAILED_COUNT++))

# macOS builds
build_target "darwin" "amd64" "" || ((FAILED_COUNT++))
build_target "darwin" "arm64" "" || ((FAILED_COUNT++))

# Windows builds
build_target "windows" "amd64" ".exe" || ((FAILED_COUNT++))
build_target "windows" "386" ".exe" || ((FAILED_COUNT++))
build_target "windows" "arm64" ".exe" || ((FAILED_COUNT++))

# FreeBSD builds
build_target "freebsd" "amd64" "" || ((FAILED_COUNT++))
build_target "freebsd" "arm64" "" || ((FAILED_COUNT++))

# NetBSD builds
build_target "netbsd" "amd64" "" || ((FAILED_COUNT++))
build_target "netbsd" "arm64" "" || ((FAILED_COUNT++))

# OpenBSD builds
build_target "openbsd" "amd64" "" || ((FAILED_COUNT++))
build_target "openbsd" "arm64" "" || ((FAILED_COUNT++))

# Solaris builds
build_target "solaris" "amd64" "" || ((FAILED_COUNT++))

# Calculate total and success counts
TOTAL_COUNT=$(ls -1 "$OUTPUT_DIR" 2>/dev/null | wc -l | tr -d ' ')
SUCCESS_COUNT=$((TOTAL_COUNT - FAILED_COUNT))

# Summary
echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Build Summary${NC}"
echo -e "${GREEN}========================================${NC}"
echo -e "Total builds: $TOTAL_COUNT"
echo -e "${GREEN}Successful: $SUCCESS_COUNT${NC}"
if [ $FAILED_COUNT -gt 0 ]; then
    echo -e "${RED}Failed: $FAILED_COUNT${NC}"
fi
echo -e "${YELLOW}Output location: $OUTPUT_DIR${NC}"
echo ""

# List all built binaries
echo "Built binaries:"
ls -lh "$OUTPUT_DIR" | tail -n +2 | awk '{print "  " $9 " (" $5 ")"}'

echo ""
echo -e "${GREEN}Build complete!${NC}"

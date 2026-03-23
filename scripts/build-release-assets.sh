#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: ./scripts/build-release-assets.sh <tag> [output-dir]

Build release archives for common Raijin targets and write a SHA256SUMS manifest.
EOF
}

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
    usage >&2
    exit 1
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

TAG="$1"
OUT_DIR_INPUT="${2:-dist}"
case "$OUT_DIR_INPUT" in
    /*) OUT_DIR="$OUT_DIR_INPUT" ;;
    *) OUT_DIR="$ROOT_DIR/$OUT_DIR_INPUT" ;;
esac
VERSION="${TAG#v}"

if [ "$VERSION" = "$TAG" ]; then
    echo "tag must start with v (got: $TAG)" >&2
    exit 1
fi

VERSION_FILE="$ROOT_DIR/internal/version/VERSION"
FILE_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")
if [ "$FILE_VERSION" != "$VERSION" ]; then
    echo "version mismatch: tag=$VERSION file=$FILE_VERSION" >&2
    exit 1
fi

if ! command -v zip >/dev/null 2>&1; then
    echo "zip is required to build Windows archives" >&2
    exit 1
fi

mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR"/raijin_* "$OUT_DIR"/SHA256SUMS

targets=(
    "darwin amd64 tar.gz"
    "darwin arm64 tar.gz"
    "linux amd64 tar.gz"
    "linux arm64 tar.gz"
    "windows amd64 zip"
    "windows arm64 zip"
)

artifacts=()

for target in "${targets[@]}"; do
    read -r goos goarch ext <<< "$target"
    stage_dir=$(mktemp -d)
    archive_base="raijin_${VERSION}_${goos}_${goarch}"
    binary_name="raijin"
    if [ "$goos" = "windows" ]; then
        binary_name="raijin.exe"
    fi

    echo "building $goos/$goarch"
    GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
        go build -trimpath -ldflags="-s -w" -o "$stage_dir/$binary_name" .

    cp README.md LICENSE "$stage_dir"/

    artifact_path="$OUT_DIR/$archive_base.$ext"
    if [ "$ext" = "zip" ]; then
        (
            cd "$stage_dir"
            zip -q "$artifact_path" "$binary_name" README.md LICENSE
        )
    else
        tar -C "$stage_dir" -czf "$artifact_path" "$binary_name" README.md LICENSE
    fi

    artifacts+=("$(basename "$artifact_path")")
    rm -rf "$stage_dir"
done

checksum_cmd=()
if command -v sha256sum >/dev/null 2>&1; then
    checksum_cmd=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
    checksum_cmd=(shasum -a 256)
else
    echo "sha256sum or shasum is required to generate checksums" >&2
    exit 1
fi

(
    cd "$OUT_DIR"
    "${checksum_cmd[@]}" "${artifacts[@]}" > SHA256SUMS
)

echo "artifacts written to $OUT_DIR"

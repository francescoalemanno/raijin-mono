#!/bin/sh
# Raijin installer (build from latest release source)
# Usage: curl -fsSL https://raw.githubusercontent.com/francescoalemanno/raijin-mono/main/scripts/install.sh | sh

set -e

REPO="francescoalemanno/raijin-mono"
BINARY_NAME="raijin"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Check prerequisites
if ! command -v curl >/dev/null 2>&1; then
    echo "Error: curl is required"
    exit 1
fi

if ! command -v tar >/dev/null 2>&1; then
    echo "Error: tar is required"
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go is required to build raijin from source"
    echo "Install Go from: https://go.dev/dl/"
    exit 1
fi

# Resolve latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

if [ -z "$TAG" ]; then
    echo "Could not determine latest release tag."
    exit 1
fi

# IMPORTANT: source is fetched from the latest release tag
SOURCE_URL="https://github.com/${REPO}/archive/refs/tags/${TAG}.tar.gz"

# Create install dir if needed
mkdir -p "$INSTALL_DIR"

echo "Installing raijin ${TAG} from source to ${INSTALL_DIR}..."

# Prepare temp build workspace
TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

ARCHIVE_PATH="$TMP_DIR/source.tar.gz"

# Download source archive for latest release
curl -fsSL "$SOURCE_URL" -o "$ARCHIVE_PATH"

# Extract source
tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"

# The extracted folder is usually raijin-mono-${TAG#v}
SRC_DIR="$TMP_DIR/raijin-mono-${TAG#v}"
if [ ! -d "$SRC_DIR" ]; then
    # Fallback in case tag naming has a different format
    SRC_DIR=$(find "$TMP_DIR" -maxdepth 1 -type d -name 'raijin-mono-*' | head -n 1)
fi

if [ -z "$SRC_DIR" ] || [ ! -d "$SRC_DIR" ]; then
    echo "Could not locate extracted source directory."
    exit 1
fi

# Build binary
(
    cd "$SRC_DIR"
    CGO_ENABLED=0 go build -o "$TMP_DIR/$BINARY_NAME" .
)

chmod +x "$TMP_DIR/$BINARY_NAME"
mv "$TMP_DIR/$BINARY_NAME" "${INSTALL_DIR}/${BINARY_NAME}"

echo "raijin ${TAG} installed to ${INSTALL_DIR}/${BINARY_NAME}"

# Add INSTALL_DIR to PATH in shell config files if not already present
PATH_LINE="export PATH=\"\$PATH:${INSTALL_DIR}\""
ADDED=0

add_to_shell() {
    cfg="$1"
    if [ -f "$cfg" ] || [ "$2" = "force" ]; then
        if ! grep -qF "${INSTALL_DIR}" "$cfg" 2>/dev/null; then
            printf '\n# Added by raijin installer\n%s\n' "$PATH_LINE" >> "$cfg"
            echo "Added ${INSTALL_DIR} to PATH in $cfg"
            ADDED=1
        fi
    fi
}

add_to_shell "$HOME/.bashrc"
add_to_shell "$HOME/.zshrc"
add_to_shell "$HOME/.profile" force   # fallback for POSIX shells / login shells

if [ "$ADDED" -eq 1 ]; then
    echo ""
    echo "PATH updated. Restart your shell or run:"
    echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
else
    echo "${INSTALL_DIR} is already in your shell config."
fi

echo ""
echo "Run 'raijin --help' to get started."
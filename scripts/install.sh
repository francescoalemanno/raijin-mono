#!/bin/sh
# Raijin installer
# Usage: curl -fsSL https://raw.githubusercontent.com/francescoalemanno/raijin-mono/main/scripts/install.sh | sh

set -e

REPO="francescoalemanno/raijin-mono"
BINARY_NAME="raijin"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64 | amd64) ARCH="amd64" ;;
    aarch64 | arm64) ARCH="arm64" ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Resolve latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

if [ -z "$TAG" ]; then
    echo "Could not determine latest release tag."
    exit 1
fi

ASSET="${BINARY_NAME}-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"

# Create install dir if needed
mkdir -p "$INSTALL_DIR"

echo "Installing raijin ${TAG} (${OS}/${ARCH}) to ${INSTALL_DIR}..."

# Download
TMP=$(mktemp)
curl -fsSL "$URL" -o "$TMP"
chmod +x "$TMP"
mv "$TMP" "${INSTALL_DIR}/${BINARY_NAME}"

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

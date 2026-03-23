#!/bin/sh
# Raijin installer (download the latest prebuilt release binary)
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

# Resolve latest release tag
echo "Fetching latest release..."
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

if [ -z "$TAG" ]; then
    echo "Could not determine latest release tag."
    exit 1
fi

OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
    Linux) GOOS="linux" ;;
    Darwin) GOOS="darwin" ;;
    *)
        echo "Unsupported operating system: ${OS}"
        echo "Download a matching archive manually from https://github.com/${REPO}/releases/latest"
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64|amd64) GOARCH="amd64" ;;
    arm64|aarch64) GOARCH="arm64" ;;
    *)
        echo "Unsupported architecture: ${ARCH}"
        echo "Download a matching archive manually from https://github.com/${REPO}/releases/latest"
        exit 1
        ;;
esac

ASSET_NAME="raijin_${TAG#v}_${GOOS}_${GOARCH}.tar.gz"
ASSET_URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET_NAME}"

# Create install dir if needed
mkdir -p "$INSTALL_DIR"

echo "Installing raijin ${TAG} (${GOOS}/${GOARCH}) to ${INSTALL_DIR}..."

# Prepare temp build workspace
TMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

ARCHIVE_PATH="$TMP_DIR/${ASSET_NAME}"

if ! curl -fsSL "$ASSET_URL" -o "$ARCHIVE_PATH"; then
    echo "Could not download release asset: ${ASSET_NAME}"
    echo "Check https://github.com/${REPO}/releases/tag/${TAG} for available assets."
    exit 1
fi

tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"

if [ ! -f "$TMP_DIR/$BINARY_NAME" ]; then
    echo "Could not locate extracted binary."
    exit 1
fi

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

# Best-effort first-run setup (shell integration + initial model setup flow).
SETUP_SHELL=$(basename "${SHELL:-}")
case "$SETUP_SHELL" in
    zsh|bash|fish)
        echo "Running automatic setup: ${BINARY_NAME} /setup ${SETUP_SHELL}"
        if "${INSTALL_DIR}/${BINARY_NAME}" "/setup" "${SETUP_SHELL}"; then
            echo "Automatic setup completed."
        else
            echo "Automatic setup did not fully complete."
            echo "Run '${BINARY_NAME} /setup ${SETUP_SHELL}' in an interactive terminal to finish setup."
        fi
        ;;
    *)
        echo "Skipping automatic setup (unsupported or unknown SHELL: ${SHELL:-<empty>})."
        echo "Run '${BINARY_NAME} /setup [zsh|bash|fish]' in your shell to finish setup."
        ;;
esac

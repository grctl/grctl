#!/bin/sh
set -e

REPO="cemevren/grctl"
INSTALL_DIR="/usr/local/bin"

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS" && exit 1 ;;
esac

# Get latest server release tag
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
  | grep '"tag_name"' \
  | grep '"grctl/server-' \
  | head -1 \
  | sed 's/.*"tag_name": "\(.*\)".*/\1/')

if [ -z "$TAG" ]; then
  echo "Could not find a server release. Have any releases been published yet?"
  exit 1
fi

ARCHIVE="grctl-${OS}-${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

echo "Installing grctl ${TAG} (${OS}/${ARCH})..."

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" -o "${TMP}/${ARCHIVE}"
tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"

# Use ~/.local/bin as fallback if /usr/local/bin is not writable
if [ ! -w "$INSTALL_DIR" ]; then
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
fi

mv "${TMP}/grctld" "${INSTALL_DIR}/grctld"
mv "${TMP}/grctl" "${INSTALL_DIR}/grctl"
chmod +x "${INSTALL_DIR}/grctld" "${INSTALL_DIR}/grctl"

echo "Installed grctld and grctl to ${INSTALL_DIR}"

if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
  echo "Note: ${INSTALL_DIR} is not in your PATH. Add it to your shell profile."
fi

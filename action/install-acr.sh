#!/bin/bash
set -euo pipefail

# install-acr.sh — Download and install the ACR binary for GitHub Actions.
# Usage: ./install-acr.sh [version]
# version: "latest" (default) or a specific version like "v1.2.3"

VERSION="${1:-latest}"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

REPO="richhaase/agentic-code-reviewer"

if [ "$VERSION" = "latest" ]; then
  VERSION=$(gh release view --repo "$REPO" --json tagName -q '.tagName')
fi

ARCHIVE="acr_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE}"

echo "Installing ACR ${VERSION} (${OS}/${ARCH})..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -sL "$URL" -o "$TMPDIR/$ARCHIVE"
tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR"

install -m 755 "$TMPDIR/acr" /usr/local/bin/acr

echo "ACR ${VERSION} installed successfully"
acr --version

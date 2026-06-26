#!/usr/bin/env bash
set -euo pipefail

REPO="benoybose/qodex"
BINARY="qodex"

usage() {
  cat <<EOF
Usage: install.sh [--version <ver>] [--dir <path>]

Install the qodex binary from GitHub Releases.

Options:
  --version <ver>   Version to install (default: latest)
  --dir <path>      Installation directory (default: /usr/local/bin)
  --help            Show this message
EOF
  exit 0
}

VERSION="latest"
INSTALL_DIR="/usr/local/bin"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --dir)     INSTALL_DIR="$2"; shift 2 ;;
    --help|-h) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

# Detect OS and arch
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux)  OS="linux" ;;
  Darwin) OS="darwin" ;;
  *)      echo "Unsupported OS: $OS"; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="x86_64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)            echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/$REPO/releases/latest/download/${BINARY}_${OS}_${ARCH}.tar.gz"
else
  URL="https://github.com/$REPO/releases/download/v${VERSION}/${BINARY}_${OS}_${ARCH}.tar.gz"
fi

echo "Downloading qodex $VERSION ($OS/$ARCH)..."
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

if command -v curl &>/dev/null; then
  curl -fsSL "$URL" -o "$TMPDIR/qodex.tar.gz"
elif command -v wget &>/dev/null; then
  wget -q "$URL" -O "$TMPDIR/qodex.tar.gz"
else
  echo "Need curl or wget to download."
  exit 1
fi

tar -xzf "$TMPDIR/qodex.tar.gz" -C "$TMPDIR"
mkdir -p "$INSTALL_DIR"
mv "$TMPDIR/qodex" "$INSTALL_DIR/qodex"
chmod +x "$INSTALL_DIR/qodex"

echo "Installed qodex to $INSTALL_DIR/qodex"
echo "Run 'qodex version' to verify."

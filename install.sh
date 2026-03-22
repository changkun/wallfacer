#!/bin/sh
# Wallfacer installer — detects OS/arch and downloads the latest release binary.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/changkun/wallfacer/main/install.sh | sh
#
# Environment variables:
#   WALLFACER_INSTALL_DIR  — where to place the binary (default: /usr/local/bin or ~/.local/bin)
#   WALLFACER_VERSION      — version to install (default: latest)

set -e

REPO="changkun/wallfacer"

# --- Detect OS ---
OS="$(uname -s)"
case "$OS" in
  Linux)   OS="linux" ;;
  Darwin)  OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *)
    echo "Error: unsupported operating system: $OS" >&2
    echo "Wallfacer supports Linux, macOS, and Windows." >&2
    exit 1
    ;;
esac

# --- Detect architecture ---
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64)  ARCH="arm64" ;;
  *)
    echo "Error: unsupported architecture: $ARCH" >&2
    echo "Wallfacer supports amd64 and arm64." >&2
    exit 1
    ;;
esac

# --- Determine version ---
if [ -n "$WALLFACER_VERSION" ]; then
  VERSION="$WALLFACER_VERSION"
else
  VERSION="latest"
fi

# --- Determine install directory ---
if [ -n "$WALLFACER_INSTALL_DIR" ]; then
  INSTALL_DIR="$WALLFACER_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="$HOME/.local/bin"
fi

# --- Windows constraints ---
EXT=""
if [ "$OS" = "windows" ]; then
  EXT=".exe"
  if [ "$ARCH" != "amd64" ]; then
    echo "Error: Windows builds are available for amd64 only." >&2
    exit 1
  fi
fi

# --- Build download URL ---
BINARY="wallfacer-${OS}-${ARCH}${EXT}"
if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"
else
  URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}"
fi

echo "Downloading wallfacer (${OS}/${ARCH})..."
echo "  ${URL}"

# --- Download ---
TMPFILE="$(mktemp)"
trap 'rm -f "$TMPFILE"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fSL --progress-bar "$URL" -o "$TMPFILE"
elif command -v wget >/dev/null 2>&1; then
  wget -q --show-progress "$URL" -O "$TMPFILE"
else
  echo "Error: curl or wget is required." >&2
  exit 1
fi

# --- Install ---
mkdir -p "$INSTALL_DIR"
chmod +x "$TMPFILE"
mv "$TMPFILE" "$INSTALL_DIR/wallfacer${EXT}"
trap - EXIT

echo ""
echo "Installed wallfacer to ${INSTALL_DIR}/wallfacer${EXT}"

# --- Check PATH ---
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "Note: ${INSTALL_DIR} is not in your PATH."
    echo "Add it with:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac

echo ""
echo "Get started:"
echo "  wallfacer run"

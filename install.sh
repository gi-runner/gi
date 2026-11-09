#!/bin/sh
# Install gi. A published release is unpacked into /usr/local/bin, or the
# module is built with go when the release is not there yet.
set -eu

version="${GI_VERSION:-1.1.1}"
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin|linux) ;;
  *) echo "On Windows, download the zip from the GitHub releases page." >&2; exit 1 ;;
esac

dest="${GI_INSTALL_DIR:-/usr/local/bin}"
mkdir -p "$dest"
url="https://github.com/gi-runner/gi/releases/download/v${version}/gi_${version}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if curl -fsSL "$url" -o "$tmp/gi.tar.gz"; then
  tar -xzf "$tmp/gi.tar.gz" -C "$tmp"
  install -m 755 "$tmp/gi" "$dest/gi"
  echo "installed $dest/gi"
  echo "next: gi demo"
  exit 0
fi

echo "release v${version} is not published; building with go" >&2
if ! command -v go >/dev/null 2>&1; then
  echo "install Go 1.27.2 or newer, then run this script again" >&2
  exit 1
fi
GOBIN="$dest" go install "github.com/gi-runner/gi/cmd/gi@v${version}" || GOBIN="$dest" go install github.com/gi-runner/gi/cmd/gi@latest
echo "installed $dest/gi"
echo "next: gi demo"

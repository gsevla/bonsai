#!/bin/sh
set -eu

repo="gsevla/bonsai"
install_dir="${BONSAI_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)

case "$os" in
  darwin|linux) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

case "$arch" in
  x86_64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

archive="bonsai_${os}_${arch}.tar.gz"
base="${BONSAI_BASE_URL:-https://github.com/$repo/releases/latest/download}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

cd "$tmp"
if command -v sha256sum >/dev/null 2>&1; then
  grep " $archive\$" checksums.txt | sha256sum -c - >/dev/null
else
  grep " $archive\$" checksums.txt | shasum -a 256 -c - >/dev/null
fi

tar -xzf "$archive" bonsai
mkdir -p "$install_dir"
install -m 755 bonsai "$install_dir/bonsai"
echo "installed bonsai to $install_dir/bonsai"

case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "add $install_dir to your PATH" ;;
esac

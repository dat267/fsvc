#!/bin/sh
# Install the fsvc binary onto PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/dat267/fsvc/main/install.sh | sh
#
# FSVC_INSTALL_DIR  target directory (default: ~/.local/bin)
# FSVC_VERSION      release tag to install (default: latest)
set -eu

repo="dat267/fsvc"
dir="${FSVC_INSTALL_DIR:-$HOME/.local/bin}"
version="${FSVC_VERSION:-latest}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
suffix=""
case "$os" in
  linux | darwin) ;;
  mingw* | msys* | cygwin*) os=windows; suffix=".exe" ;;
  *) echo "fsvc: unsupported OS '$os'; download the binary manually" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  i386 | i486 | i586 | i686) arch=386 ;;
  *) echo "fsvc: unsupported architecture '$arch'; download the binary manually" >&2; exit 1 ;;
esac

asset="fsvc_${os}_${arch}${suffix}"
if [ "$version" = "latest" ]; then
  url="https://github.com/${repo}/releases/latest/download/${asset}"
else
  url="https://github.com/${repo}/releases/download/${version}/${asset}"
fi

mkdir -p "$dir"
tmp="$dir/.fsvc-download.$$"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL -o "$tmp" "$url"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$tmp" "$url"
else
  echo "fsvc: need curl or wget to download ${url}" >&2
  exit 1
fi
chmod +x "$tmp"
mv "$tmp" "$dir/fsvc${suffix}"

echo "installed $("$dir/fsvc${suffix}" version) to $dir/fsvc${suffix}"
case ":${PATH}:" in
  *":${dir}:"*) ;;
  *) echo "note: ${dir} is not on your PATH" ;;
esac

#!/usr/bin/env bash
set -euo pipefail

# Pin the binary release and its published SHA-256 digests for local and CI use.
# https://github.com/golangci/golangci-lint/releases/tag/v2.14.0
version=2.14.0
install_dir="${1:-.tools/bin}"

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64)
    platform=darwin-arm64
    checksum=5ef5f36a7147e91dc58ef9ef4d11bb7bad5ead0c76eb6c01327a73c641d1dcc3
    ;;
  Darwin-x86_64)
    platform=darwin-amd64
    checksum=a5667c1c3536be1740133213e1e822bfb8f0d98ea12903174d6d5f635e4ed68d
    ;;
  Linux-aarch64|Linux-arm64)
    platform=linux-arm64
    checksum=ee7ec5f3453d15ddf106fae5a4d6c71737712348a979d1fe9cd52ec7ea299bae
    ;;
  Linux-x86_64)
    platform=linux-amd64
    checksum=ab90aeb7b066f92a33415b638a50fe5344bbb75a0d32ad30cc248d88f81032ab
    ;;
  *)
    echo "Unsupported platform; install golangci-lint v$version manually." >&2
    exit 1
    ;;
esac

download_dir="$(mktemp -d)"
trap 'rm -rf "$download_dir"' EXIT
release_dir="golangci-lint-${version}-${platform}"
archive="$release_dir.tar.gz"
curl --fail --silent --show-error --location --retry 3 \
  "https://github.com/golangci/golangci-lint/releases/download/v${version}/${archive}" \
  --output "$download_dir/$archive"

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "$download_dir/$archive")"
else
  actual_checksum="$(shasum -a 256 "$download_dir/$archive")"
fi
if [ "${actual_checksum%% *}" != "$checksum" ]; then
  echo "golangci-lint download checksum mismatch; refusing to install." >&2
  exit 1
fi

tar -xzf "$download_dir/$archive" -C "$download_dir" "$release_dir/golangci-lint"
mkdir -p "$install_dir"
install -m 755 "$download_dir/$release_dir/golangci-lint" "$install_dir/golangci-lint"
"$install_dir/golangci-lint" version

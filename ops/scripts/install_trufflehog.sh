#!/usr/bin/env bash
set -euo pipefail

# Pin an aged release and its published SHA-256 digests, rather than running a
# remote installer or downloading "latest".
# https://github.com/trufflesecurity/trufflehog/releases/tag/v3.97.9
version=3.97.9
install_dir="${1:-.tools/bin}"

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64)
    platform=darwin_arm64
    checksum=3d25c178fe1a2563687b22547adcaebcf08cc8a607315c6741fe396c597951f7
    ;;
  Darwin-x86_64)
    platform=darwin_amd64
    checksum=4451ba8d17106da0d0a2f1952d3b1c2d20a6e5c97c21ccd1f8fc04ecf2ff77d9
    ;;
  Linux-aarch64|Linux-arm64)
    platform=linux_arm64
    checksum=372c568695d49e53517075b5f74d1ee9a19053f13661d689c7928ee1fc27d705
    ;;
  Linux-x86_64)
    platform=linux_amd64
    checksum=40377e6572495412fb9ba0bc21c9401f73b72f1d2afd11b9931bc4a5ed622866
    ;;
  *)
    echo "Unsupported platform; install TruffleHog manually." >&2
    exit 1
    ;;
esac

download_dir="$(mktemp -d)"
trap 'rm -rf "$download_dir"' EXIT
archive="trufflehog_${version}_${platform}.tar.gz"
curl --fail --silent --show-error --location --retry 3 \
  "https://github.com/trufflesecurity/trufflehog/releases/download/v${version}/${archive}" \
  --output "$download_dir/$archive"

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "$download_dir/$archive")"
else
  actual_checksum="$(shasum -a 256 "$download_dir/$archive")"
fi
if [ "${actual_checksum%% *}" != "$checksum" ]; then
  echo "TruffleHog download checksum mismatch; refusing to install." >&2
  exit 1
fi

tar -xzf "$download_dir/$archive" -C "$download_dir" trufflehog
mkdir -p "$install_dir"
install -m 755 "$download_dir/trufflehog" "$install_dir/trufflehog"
"$install_dir/trufflehog" --version

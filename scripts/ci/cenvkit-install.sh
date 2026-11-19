#!/bin/sh
# cenvkit-install.sh — install a PINNED cenvkit from a mirror and verify it.
#
# Required:
#   CENVKIT_VERSION   release version without the leading "v" (e.g. 0.6.2)
#   CENVKIT_SHA256    sha256 of the archive for THIS os/arch (see checksums.txt)
#   CENVKIT_MIRROR    base URL with the layout <MIRROR>/<version>/<asset>
#
# Optional:
#   CENVKIT_MIRROR_AUTH  curl header for private mirrors, e.g. "JOB-TOKEN: $CI_JOB_TOKEN"
#                        (GitLab) or "PRIVATE-TOKEN: glpat-..." (personal token)
#   CENVKIT_LOCAL_BIN    pre-baked ARCHIVE path — installs with no network access
#                        (air-gapped runners that ship the tarball inside the image)
#
# Usage: sh scripts/ci/cenvkit-install.sh [install-dir]    (default: $HOME/.local/bin)
# Exit codes: 0 installed, 2 usage/unsupported platform, 1 download or checksum failure.
set -eu

: "${CENVKIT_VERSION:?CENVKIT_VERSION is required (e.g. 0.6.2)}"
: "${CENVKIT_SHA256:?CENVKIT_SHA256 is required (pin it from checksums.txt)}"
dest=${1:-$HOME/.local/bin}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "cenvkit-install: unsupported arch $(uname -m)" >&2; exit 2 ;;
esac

ext=tar.gz
[ "$os" = "windows" ] && ext=zip
asset="cenvkit_${CENVKIT_VERSION}_${os}_${arch}.${ext}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
archive="$tmp/$asset"

if [ -n "${CENVKIT_LOCAL_BIN:-}" ]; then
  echo "cenvkit-install: using pre-baked archive $CENVKIT_LOCAL_BIN"
  cp "$CENVKIT_LOCAL_BIN" "$archive"
else
  : "${CENVKIT_MIRROR:?CENVKIT_MIRROR is required (internal artifact store)}"
  url="${CENVKIT_MIRROR%/}/$CENVKIT_VERSION/$asset"
  echo "cenvkit-install: fetching $url"
  if command -v curl >/dev/null 2>&1; then
    if [ -n "${CENVKIT_MIRROR_AUTH:-}" ]; then
      curl -fsSL --retry 3 --connect-timeout 10 -H "$CENVKIT_MIRROR_AUTH" "$url" -o "$archive"
    else
      curl -fsSL --retry 3 --connect-timeout 10 "$url" -o "$archive"
    fi
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$archive" "$url"
  else
    echo "cenvkit-install: need curl or wget" >&2; exit 2
  fi
fi

if command -v sha256sum >/dev/null 2>&1; then
  printf '%s  %s\n' "$CENVKIT_SHA256" "$archive" | sha256sum -c - >/dev/null
else
  printf '%s  %s\n' "$CENVKIT_SHA256" "$archive" | shasum -a 256 -c - >/dev/null
fi
echo "cenvkit-install: sha256 OK"

case "$archive" in
  *.tar.gz) tar -xzf "$archive" -C "$tmp" cenvkit ;;
  *.zip)    unzip -q "$archive" cenvkit -d "$tmp" ;;
  *)        echo "cenvkit-install: unsupported archive $archive" >&2; exit 2 ;;
esac

mkdir -p "$dest"
cp "$tmp/cenvkit" "$dest/cenvkit"
chmod 0755 "$dest/cenvkit"
"$dest/cenvkit" version
echo "cenvkit-install: installed $dest/cenvkit"

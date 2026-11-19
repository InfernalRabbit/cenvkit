#!/bin/sh
# cenvkit-install-selftest.sh — offline self-test for cenvkit-install.sh.
#
# Builds the CLI from this tree, packages it as a release-shaped archive, serves
# it from a file:// "mirror", and checks that the installer (a) installs when the
# pinned sha256 matches and (b) refuses when it does not. Needs no network.
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

version=0.0.0-selftest
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "selftest: unsupported arch $(uname -m)" >&2; exit 2 ;;
esac
asset="cenvkit_${version}_${os}_${arch}.tar.gz"

( cd "$repo" && go build -o "$tmp/cenvkit" ./cmd/cenvkit )
tar -czf "$tmp/$asset" -C "$tmp" cenvkit
if command -v sha256sum >/dev/null 2>&1; then
  sha=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
  sha=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
mkdir -p "$tmp/mirror/$version"
cp "$tmp/$asset" "$tmp/mirror/$version/$asset"

CENVKIT_VERSION="$version" CENVKIT_SHA256="$sha" CENVKIT_MIRROR="file://$tmp/mirror" \
  sh "$repo/scripts/ci/cenvkit-install.sh" "$tmp/out" >/dev/null
"$tmp/out/cenvkit" version >/dev/null
echo "selftest: install from mirror OK"

if CENVKIT_VERSION="$version" CENVKIT_SHA256="0000000000000000000000000000000000000000000000000000000000000000" \
     CENVKIT_MIRROR="file://$tmp/mirror" \
     sh "$repo/scripts/ci/cenvkit-install.sh" "$tmp/out-bad" >/dev/null 2>&1; then
  echo "selftest: FAILED - a wrong checksum was accepted" >&2
  exit 1
fi
[ ! -e "$tmp/out-bad/cenvkit" ] || { echo "selftest: FAILED - installed despite bad checksum" >&2; exit 1; }
echo "selftest: wrong checksum rejected OK"

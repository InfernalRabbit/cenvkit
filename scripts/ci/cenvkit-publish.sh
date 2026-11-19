#!/bin/sh
# cenvkit-publish.sh — mirror cenvkit release assets into an artifact registry.
#
# Publishes every cenvkit_* archive plus checksums.txt to:
#   <CI_API_V4_URL>/projects/<CI_PROJECT_ID>/packages/generic/cenvkit/<version>/<file>
# (GitLab Generic Package Registry; the same layout cenvkit-install.sh expects.)
#
# Required:
#   CENVKIT_VERSION     version without the leading "v"
#   CI_API_V4_URL       e.g. https://gitlab.com/api/v4
#   CI_PROJECT_ID       numeric project id that owns the package
#   CENVKIT_API_TOKEN   auth header, e.g. "JOB-TOKEN: $CI_JOB_TOKEN" (in CI)
#                       or "PRIVATE-TOKEN: glpat-..." (workstation; needs api scope)
#
# Usage: sh scripts/ci/cenvkit-publish.sh [assets-dir]    (default: dist)
set -eu

: "${CENVKIT_VERSION:?CENVKIT_VERSION is required (e.g. 0.6.2)}"
: "${CI_API_V4_URL:?CI_API_V4_URL is required (e.g. https://gitlab.com/api/v4)}"
: "${CI_PROJECT_ID:?CI_PROJECT_ID is required (numeric project id)}"
: "${CENVKIT_API_TOKEN:?CENVKIT_API_TOKEN is required (JOB-TOKEN/PRIVATE-TOKEN header)}"

assets_dir=${1:-dist}
base="$CI_API_V4_URL/projects/$CI_PROJECT_ID/packages/generic/cenvkit/$CENVKIT_VERSION"

if [ -f "$assets_dir/checksums.txt" ]; then
  # Verify what we are about to publish against the release manifest.
  if command -v sha256sum >/dev/null 2>&1; then
    ( cd "$assets_dir" && sha256sum -c checksums.txt )
  else
    ( cd "$assets_dir" && shasum -a 256 -c checksums.txt )
  fi
  echo "cenvkit-publish: local checksums OK"
fi

count=0
for f in "$assets_dir"/cenvkit_* "$assets_dir"/checksums.txt; do
  [ -f "$f" ] || continue
  name=$(basename "$f")
  printf 'cenvkit-publish: %s ... ' "$name"
  code=$(curl -sS -o /dev/null -w '%{http_code}' \
    --header "$CENVKIT_API_TOKEN" --upload-file "$f" "$base/$name")
  case "$code" in
    2*) printf 'uploaded (%s)\n' "$code" ;;
    *)  printf 'FAILED (HTTP %s)\n' "$code"; exit 1 ;;
  esac
  # A 2xx upload is not enough: prove the file is actually retrievable.
  code=$(curl -sS -o /dev/null -w '%{http_code}' --header "$CENVKIT_API_TOKEN" "$base/$name")
  case "$code" in
    2*) ;;
    *)  echo "cenvkit-publish: $name is not retrievable (HTTP $code)" >&2; exit 1 ;;
  esac
  count=$((count + 1))
done

[ "$count" -gt 0 ] || { echo "cenvkit-publish: no cenvkit_* assets in $assets_dir" >&2; exit 2; }
echo "cenvkit-publish: $count file(s) live in $base"

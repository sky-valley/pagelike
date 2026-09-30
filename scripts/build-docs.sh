#!/bin/sh
# Build the pagelike docs site.
#
#   scripts/build-docs.sh
#
# Default flags match the repo layout: --src=. and --out=site/public.
# Pass --serve to start a local preview at http://127.0.0.1:9000 after
# the build finishes.

set -eu
cd "$(dirname "$0")/.."

quiet=""
serve=0
src=.
out=site/public
base="https://sky-valley.github.io/pagelike"
while [ $# -gt 0 ]; do
  case "$1" in
    --quiet) quiet=--quiet;;
    --serve) serve=1;;
    --src) src=$2; shift;;
    --out) out=$2; shift;;
    --base-url) base=$2; shift;;
    -h|--help)
      sed -n '2,12p' "$0"
      exit 0;;
    *) echo "unknown flag: $1" >&2; exit 2;;
  esac
  shift
done

echo "build-docs: $src -> $out (base=$base)"
go run ./cmd/site build --src "$src" --out "$out" --base-url "$base"

# Run the public-content scan against the freshly-built tree. This
# re-checks managed-published content the same way ci does on the
# source repo.
sh scripts/scan-public.sh

if [ "$serve" = "1" ]; then
  go run ./cmd/site serve --out "$out"
fi

#!/bin/sh
# Definition of done: formatting, vet, every Go test (including every local
# compatibility-harness case and the durability tests), the public-content
# scan, and the browser suites.
#
#   scripts/check.sh            # everything
#   scripts/check.sh --quick    # skip the browser suites
set -eu
cd "$(dirname "$0")/.."
step() { printf '\n== %s\n' "$*"; }

step gofmt
unformatted=$(gofmt -l cmd internal harness test tools)
if [ -n "$unformatted" ]; then echo "$unformatted"; exit 1; fi

step "go vet"
go vet ./...

step "go test ./..."
go test ./...

step "public-content scan"
scripts/scan-public.sh

if [ "${1:-}" != "--quick" ]; then
  step "browser suites"
  (cd e2e && { [ -d node_modules ] || npm ci; } && npx playwright test)
fi
printf '\nall checks passed\n'

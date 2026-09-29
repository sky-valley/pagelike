#!/bin/sh
# Build release archives of pagelike for every supported platform.
#
#   scripts/release.sh v0.1.0          # writes dist/
#
# Binaries are static (CGO_ENABLED=0; SQLite and QuickJS are pure Go), built
# with -trimpath and the version stamped into `pagelike version`.
set -eu
version=${1:?usage: scripts/release.sh vX.Y.Z}
root=$(cd "$(dirname "$0")/.." && pwd)
dist="$root/dist"
rm -rf "$dist"
mkdir -p "$dist"
cd "$root"

targets="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64"
for t in $targets; do
  os=${t%/*}
  arch=${t#*/}
  name="pagelike_${version#v}_${os}_${arch}"
  stage="$dist/$name"
  mkdir -p "$stage"
  bin=pagelike
  [ "$os" = windows ] && bin=pagelike.exe
  echo "building $name"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=$version" -o "$stage/$bin" ./cmd/pagelike
  cp LICENSE NOTICE README.md CHANGELOG.md "$stage/"
  if [ "$os" = windows ]; then
    (cd "$dist" && zip -qr "$name.zip" "$name")
  else
    tar -C "$dist" -czf "$dist/$name.tar.gz" "$name"
  fi
  rm -rf "$stage"
done

cd "$dist"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -- *.tar.gz *.zip > SHA256SUMS
else
  shasum -a 256 -- *.tar.gz *.zip > SHA256SUMS
fi
cat SHA256SUMS

#!/bin/sh
# Clone the public PageLove repositories that the release acceptance suite
# (e2e/tests/apps) runs, at the commits pagelike was tested against
# (research/COMMITS.txt), into research/upstream/.
#
#   tools/research/clone_upstream.sh
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
dest="$root/research/upstream"
mkdir -p "$dest"
grep -E '^[a-z0-9-]+ +[0-9a-f]{40} ' "$root/research/COMMITS.txt" | while read -r name commit _; do
  if [ ! -d "$dest/$name/.git" ]; then
    git clone --quiet "https://github.com/pagelove/$name.git" "$dest/$name"
  fi
  git -C "$dest/$name" fetch --quiet origin "$commit" 2>/dev/null || true
  git -C "$dest/$name" checkout --quiet "$commit"
  echo "$name @ $(echo "$commit" | cut -c1-7)"
done

#!/bin/sh
# Fail if any tracked (or staged) file contains material that must not be
# published: home-directory paths, credential-shaped strings, or private
# terms. Private terms are listed one extended regex per line in
# .private-terms (git-ignored, never committed), so the list itself stays
# private.
#
#   scripts/scan-public.sh
set -eu
cd "$(dirname "$0")/.."
patterns='/Users/[a-z]
/home/[a-z]+/
pk_[0-9a-f]{8,}
ghp_[A-Za-z0-9]{20,}
github_pat_[A-Za-z0-9_]{20,}
sk-[A-Za-z0-9]{20,}
AKIA[0-9A-Z]{16}
-----BEGIN [A-Z ]*PRIVATE KEY-----
xox[abpr]-[A-Za-z0-9-]{10,}
PAGELOVE_API_KEY=[A-Za-z0-9]'
if [ -f .private-terms ]; then
  patterns="$patterns
$(grep -v -e '^#' -e '^[[:space:]]*$' .private-terms)"
fi
hits=$(mktemp)
failed=$(mktemp)
trap 'rm -f "$hits" "$failed"' EXIT
echo "$patterns" | while IFS= read -r p; do
  [ -z "$p" ] && continue
  # Only tracked or staged files; -I skips binaries. The built-in pattern
  # list matches itself, so this script is excluded.
  if git grep --cached -I -n -E -e "$p" -- . ':!scripts/scan-public.sh' >"$hits" 2>/dev/null; then
    echo "found /$p/:"
    head -5 "$hits"
    echo x >>"$failed"
  fi
done
if [ -s "$failed" ]; then
  echo "public-content scan: FAILED"
  exit 1
fi
echo "public-content scan: clean"

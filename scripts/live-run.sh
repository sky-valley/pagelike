#!/bin/sh
# Run harness cases against live PageLove, politely.
#
#   scripts/live-run.sh <label> --ids a,b,c        # or --ids @file, or --filter SUBSTR
#
# - Refuses unless .secrets/pagelove.env marks the host disposable (the file
#   is checked, never printed).
# - Refuses more than LIVE_MAX_CASES cases (default 40) per run.
# - Writes observations to harness/observations/live-<date>-<label>/ and
#   records that directory in harness/observations/ORDER, which
#   scripts/matrix.sh reads (later directories override earlier ones).
# The harness itself rate-limits to 3 requests/s.
set -eu
cd "$(dirname "$0")/.."
label=${1:?usage: scripts/live-run.sh <label> --ids … | --filter …}
shift
[ $# -gt 0 ] || { echo "select cases with --ids or --filter" >&2; exit 2; }
if ! grep -q '^PAGELOVE_DISPOSABLE=yes' .secrets/pagelove.env 2>/dev/null; then
  echo "refusing: .secrets/pagelove.env must name a disposable host (PAGELOVE_DISPOSABLE=yes)" >&2
  exit 2
fi
n=$(go run ./harness/cmd/harness list "$@" 2>/dev/null | grep -c . || true)
max=${LIVE_MAX_CASES:-40}
if [ "$n" -eq 0 ]; then echo "no cases selected" >&2; exit 2; fi
if [ "$n" -gt "$max" ]; then
  echo "refusing: $n cases selected, more than LIVE_MAX_CASES=$max" >&2
  exit 2
fi
dir="harness/observations/live-$(date +%F)-$label"
echo "running $n cases live → $dir"
grep -qx "$(basename "$dir")" harness/observations/ORDER 2>/dev/null || basename "$dir" >> harness/observations/ORDER
exec go run ./harness/cmd/harness run --target live --observations "$dir" "$@"

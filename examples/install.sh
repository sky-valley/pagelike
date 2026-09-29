#!/bin/sh
# Install an example experience into a pagelike site over the authoring plane.
#   examples/install.sh <example> <site> [base-url]
# Creates the site if needed (with embeddable session cookies for iframes),
# mints a site-scoped authoring key (kept only in this process), and uploads
# every file under examples/<example>/site/.
set -eu
EXAMPLE=$1
SITE=$2
PORT_ORIGIN=${3:-http://localhost:8787}
DATA=${PAGELIKE_DATA:-./data}
BIN=${PAGELIKE_BIN:-./bin/pagelike}
DIR=$(dirname "$0")/$EXAMPLE/site
PORT=$(printf '%s' "$PORT_ORIGIN" | sed -E 's#.*:([0-9]+)$#\1#')
if ! "$BIN" site list --data "$DATA" | grep -qx "$SITE"; then
  "$BIN" site create "$SITE" --data "$DATA"
  # Experiences are embedded in iframes: keep sessions working cross-site.
  "$BIN" identity set --site "$SITE" --cookies partitioned --data "$DATA" >/dev/null
fi
KEY=$("$BIN" key create --site "$SITE" --label "install $EXAMPLE" --ttl 1h --data "$DATA" 2>/dev/null)
( cd "$DIR" && find . -type f ) | sed 's#^\./##' | while read -r f; do
  case "$f" in
    *.html) ct=text/html ;; *.css) ct=text/css ;; *.js|*.mjs) ct=text/javascript ;;
    *.svg) ct=image/svg+xml ;; *.png) ct=image/png ;; *.jpg|*.jpeg) ct=image/jpeg ;; *) ct=application/octet-stream ;;
  esac
  code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "http://dav-$SITE.localhost:$PORT/$f" \
    -H "Authorization: Bearer $KEY" -H "Content-Type: $ct" --data-binary "@$DIR/$f")
  echo "PUT /$f -> $code"
done
echo "installed $EXAMPLE at http://$SITE.localhost:$PORT/"

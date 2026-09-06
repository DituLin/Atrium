#!/usr/bin/env bash
# Shared setup for the Atrium operator scripts.
#
# Environment:
#   ATRIUM_URL          base URL of the server (default https://127.0.0.1:8443)
#   ATRIUM_ADMIN_TOKEN  admin token; falls back to $ATRIUM_DATA_DIR/admin.token
#   ATRIUM_CA           CA certificate to trust (default $ATRIUM_DATA_DIR/tls/ca.crt)
#   ATRIUM_DATA_DIR     data directory (default ~/Library/Application Support/Atrium)
#
# Nothing here writes a credential to disk or to the process table: the token
# travels in a header read from the environment.

set -euo pipefail

ATRIUM_URL="${ATRIUM_URL:-https://127.0.0.1:8443}"
ATRIUM_DATA_DIR="${ATRIUM_DATA_DIR:-$HOME/Library/Application Support/Atrium}"

if [ -z "${ATRIUM_ADMIN_TOKEN:-}" ] && [ -r "$ATRIUM_DATA_DIR/admin.token" ]; then
  ATRIUM_ADMIN_TOKEN="$(cat "$ATRIUM_DATA_DIR/admin.token")"
fi
if [ -z "${ATRIUM_ADMIN_TOKEN:-}" ]; then
  echo "error: set ATRIUM_ADMIN_TOKEN, or make $ATRIUM_DATA_DIR/admin.token readable" >&2
  exit 2
fi

ATRIUM_CA="${ATRIUM_CA:-$ATRIUM_DATA_DIR/tls/ca.crt}"

# atrium_curl issues an authenticated request and fails on an HTTP error.
atrium_curl() {
  # macOS ships bash 3.2, where "${arr[@]}" on an empty array trips `set -u`;
  # the ${arr[@]+...} guard keeps an unset CA from breaking the call.
  local ca_args=()
  if [ -r "$ATRIUM_CA" ]; then
    ca_args=(--cacert "$ATRIUM_CA")
  fi
  curl --silent --show-error --fail-with-body \
    ${ca_args[@]+"${ca_args[@]}"} \
    --header "Authorization: Bearer $ATRIUM_ADMIN_TOKEN" \
    --header "Accept: application/json" \
    "$@"
}

# atrium_post_json sends a JSON body to a path.
atrium_post_json() {
  local path="$1" body="$2"
  atrium_curl --request POST \
    --header "Content-Type: application/json" \
    --data "$body" \
    "$ATRIUM_URL$path"
}

# atrium_get fetches a path.
atrium_get() {
  atrium_curl "$ATRIUM_URL$1"
}

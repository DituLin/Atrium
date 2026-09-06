#!/usr/bin/env bash
# List paired screens with their presence.
#
#   ./scripts/screens-list.sh
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"

atrium_get "/api/v1/screens"
echo

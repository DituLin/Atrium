#!/usr/bin/env bash
# Re-read data and re-render, keeping the current page and pause state.
#
#   ./scripts/screen-refresh.sh living_room_tv
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"

screen="${1:?usage: screen-refresh.sh <screen-id>}"

atrium_post_json "/api/v1/screens/$screen/commands" \
  '{"kind":"refresh","payload":{}}'
echo

#!/usr/bin/env bash
# Send a screen to a whitelisted local page.
#
#   ./scripts/screen-navigate.sh living_room_tv dashboard
#   ./scripts/screen-navigate.sh living_room_tv photos recent
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"

screen="${1:?usage: screen-navigate.sh <screen-id> <dashboard|photos> [collection]}"
route="${2:?usage: screen-navigate.sh <screen-id> <dashboard|photos> [collection]}"
collection="${3:-}"

if [ -n "$collection" ]; then
  payload="{\"route\":\"$route\",\"collection\":\"$collection\"}"
else
  payload="{\"route\":\"$route\"}"
fi

atrium_post_json "/api/v1/screens/$screen/commands" \
  "{\"kind\":\"navigate\",\"payload\":$payload}"
echo

#!/usr/bin/env bash
# Display one authorized photo and pause the slideshow.
#
#   ./scripts/screen-show.sh living_room_tv 01JPHOTOID
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"

screen="${1:?usage: screen-show.sh <screen-id> <photo-id>}"
photo="${2:?usage: screen-show.sh <screen-id> <photo-id>}"

atrium_post_json "/api/v1/screens/$screen/commands" \
  "{\"kind\":\"show\",\"payload\":{\"photo_id\":\"$photo\"}}"
echo

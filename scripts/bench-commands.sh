#!/usr/bin/env bash
# Conservative CLI round-trip timing, including process startup and polling.
# Usage: ./scripts/bench-commands.sh [count] [screen-id] [outfile]
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"
export ATRIUM_ADMIN_TOKEN ATRIUM_URL ATRIUM_DATA_DIR
exec python3 "$(dirname "$0")/bench_commands.py" "$@"

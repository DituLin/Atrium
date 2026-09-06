#!/usr/bin/env bash
# Measure screen control against the PRD §7.1 target: P95 <= 1 s from API
# acceptance to the client's `applied` acknowledgement, all samples <= 3 s.
#
#   ./scripts/bench-commands.sh [count] [screen-id] [outfile]
#
# The screen must be online and its previews already generated; a run that
# includes preview generation measures the media pipeline, not screen control.
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"

count="${1:-100}"
screen="${2:-living_room_tv}"
outfile="${3:-bench-commands-$(date -u +%Y%m%dT%H%M%SZ).txt}"
atrium_bin="${ATRIUM_BIN:-atrium}"

# The CLI is given the URL explicitly so the benchmark does not depend on a
# config file being present on the machine running it.
export ATRIUM_ADMIN_TOKEN

times="$(mktemp)"
applied=0
other=0

for i in $(seq 1 "$count"); do
  # Alternate the target so each command is a real state change rather than a
  # no-op the client can short-circuit.
  if [ $((i % 2)) -eq 0 ]; then
    route=dashboard
  else
    route=photos
  fi
  start="$(date +%s.%N)"
  if "$atrium_bin" admin screen navigate "$screen" \
      --route "$route" --wait --url "$ATRIUM_URL" >/dev/null 2>&1; then
    end="$(date +%s.%N)"
    awk -v a="$start" -v b="$end" 'BEGIN {printf "%.4f\n", b - a}' >> "$times"
    applied=$((applied + 1))
  else
    other=$((other + 1))
  fi
done

sort -n "$times" -o "$times"
summary="$(awk '{v[NR]=$1} END {
  if (NR == 0) { print "no samples"; exit }
  i50 = int(0.50 * NR + 0.5); if (i50 < 1) i50 = 1
  i95 = int(0.95 * NR + 0.5); if (i95 < 1) i95 = 1
  printf "P50=%.0fms P95=%.0fms max=%.0fms", v[i50]*1000, v[i95]*1000, v[NR]*1000
}' "$times")"

{
  echo "# Atrium screen control benchmark"
  echo "# date:    $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# screen:  $screen"
  echo "# target:  P95 <= 1000 ms, every normal sample <= 3000 ms (PRD 7.1)"
  echo "applied=$applied not_applied=$other $summary"
} | tee "$outfile"

rm -f "$times"
echo
echo "wrote $outfile"

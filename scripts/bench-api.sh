#!/usr/bin/env bash
# Measure the read API against the PRD §7.1 target: P95 <= 200 ms, errors < 1%.
#
#   ATRIUM_SCREEN_TOKEN=atr_scr_... ./scripts/bench-api.sh [requests] [outfile]
#
# Requests are issued sequentially from one client, which is the shape of the
# planned load (1 TV + 1 admin caller). Run it from a LAN client, not from the
# Mac mini, when the number is meant to describe what the TV experiences.
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"

count="${1:-1000}"
outfile="${2:-bench-api-$(date -u +%Y%m%dT%H%M%SZ).txt}"
token="${ATRIUM_SCREEN_TOKEN:-$ATRIUM_ADMIN_TOKEN}"

# macOS ships bash 3.2, where "${arr[@]}" on an empty array trips `set -u`.
ca_args=()
if [ -r "$ATRIUM_CA" ]; then
  ca_args=(--cacert "$ATRIUM_CA")
fi

# percentile <file> <p> — reads one number per line, already sorted.
percentile() {
  local file="$1" p="$2"
  awk -v p="$p" '{v[NR]=$1} END {
    if (NR == 0) { print "n/a"; exit }
    idx = int((p/100) * NR + 0.5); if (idx < 1) idx = 1; if (idx > NR) idx = NR
    printf "%.1f", v[idx] * 1000
  }' "$file"
}

bench_one() {
  local label="$1" path="$2"
  local times errors=0
  times="$(mktemp)"
  for _ in $(seq 1 "$count"); do
    local out status ms
    out="$(curl --silent --output /dev/null \
      ${ca_args[@]+"${ca_args[@]}"} \
      --header "Authorization: Bearer $token" \
      --write-out '%{http_code} %{time_total}' \
      "$ATRIUM_URL$path" || echo "000 0")"
    status="${out%% *}"
    ms="${out##* }"
    if [ "$status" != "200" ]; then
      errors=$((errors + 1))
    else
      echo "$ms" >> "$times"
    fi
  done
  sort -n "$times" -o "$times"
  local n p50 p95 rate
  n="$(wc -l < "$times" | tr -d ' ')"
  p50="$(percentile "$times" 50)"
  p95="$(percentile "$times" 95)"
  rate="$(awk -v e="$errors" -v c="$count" 'BEGIN {printf "%.2f", (e / c) * 100}')"
  printf '%-28s n=%-6s P50=%sms P95=%sms errors=%s (%s%%)\n' \
    "$label" "$n" "$p50" "$p95" "$errors" "$rate" | tee -a "$outfile"
  rm -f "$times"
}

{
  echo "# Atrium read API benchmark"
  echo "# date:     $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# url:      $ATRIUM_URL"
  echo "# requests: $count per endpoint"
  echo "# target:   P95 <= 200 ms, error rate < 1% (PRD 7.1)"
} > "$outfile"

bench_one "GET /home" "/api/v1/home"
bench_one "GET /photos?random" "/api/v1/photos?collection=random&limit=50"
bench_one "GET /nas/status" "/api/v1/nas/status"

echo
echo "wrote $outfile"

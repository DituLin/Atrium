# Acceptance measurements

Every metric in PRD §7.1, the command that measures it, and where the result
goes. Two rules from the PRD govern all of it:

1. **A performance result without a recorded baseline is not an acceptance
   result.** Fill the hardware, library and network sections of
   `docs/ops/g0-record.md` first.
2. Measure from where the claim applies. "Read API P95" describes what a LAN
   client sees, so run it from a LAN client, not from the Mac mini.

Record every number in `docs/ops/g0-record.md` under the matching heading, with
the date, the build (`atrium version`) and the client used.

## Before every run

```bash
export ATRIUM_URL=https://192.168.1.10:8443
export ATRIUM_ADMIN_TOKEN=…                 # or make admin.token readable
export ATRIUM_CA=/path/to/atrium-ca.crt     # exported with `atrium tls export-ca`
atrium version
atrium admin diag --json > diag-before.json
```

The run is only valid if `diag-before.json` shows every source `online`, the
job queue drained (`jobs.queued` at 0) and `cache.paused_reason` null. A
benchmark taken during the first index measures the indexer, not the API.

## 1. Read API — P95 ≤ 200 ms, errors < 1%

```bash
./scripts/bench-api.sh 1000 bench-api.txt
```

1,000 sequential requests each to `/api/v1/home`,
`/api/v1/photos?collection=random&limit=50` and `/api/v1/nas/status`, reporting
P50, P95 and the error rate per endpoint. Media and the first scan are
excluded by design: neither is a "read API".

Record: the three P95 values, the error rate, the client and its link (wired or
Wi-Fi, which band).

## 2. Initial dashboard — P95 ≤ 3 s

Manual, on the target TV, at least 20 cold starts. Time from opening the app to
the frame and local state being visible; photos may still be arriving.

```bash
# Between starts, prove the server side was not the variable:
curl -o /dev/null -s -w '%{time_total}\n' --cacert "$ATRIUM_CA" \
  -H "Authorization: Bearer $ATRIUM_ADMIN_TOKEN" "$ATRIUM_URL/api/v1/home"
```

Record: 20 stopwatch samples, the TV model and browser/WebView build, and
whether the bundle was cached.

## 3. Screen control — P95 ≤ 1 s, every normal sample ≤ 3 s

```bash
./scripts/bench-commands.sh 100 living_room_tv bench-commands.txt
```

100 `navigate` commands with `--wait`, measuring API acceptance to the client's
`applied` acknowledgement. The screen must be online and its previews already
generated. Timeouts count as real results, not as discarded samples.

Record: P50, P95, max, and the count of anything that was not `applied`, with
its `error_code`.

## 4. Discovered-data push — P95 ≤ 2 s

From Core state change to the TV rendering it, at least 100 samples. NAS
discovery time is measured separately (§6) and must not be folded in here.

```bash
# Each admin mutation publishes a change; time it to the TV's visible update.
atrium admin photos exclude 01JPHOTOID --reason "acceptance run"
atrium admin photos include 01JPHOTOID
```

Record: the sampling method, P50 and P95, and whether the TV was on the
dashboard or in a collection.

## 5. New photo visible — P95 ≤ 120 s

At least 5 batches of 100 supported files, each ≤ 20 MB, copied into the
authorized root. Time from the copy of a file completing to it being queryable
with a displayable preview.

```bash
# Marker for the batch, then poll until the count settles.
before=$(atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["photos"]["ready"])')
# ... copy the batch ...
while :; do
  now=$(atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["photos"]["ready"])')
  echo "$(date -u +%H:%M:%S) ready=$now"
  [ "$now" -ge "$((before + 100))" ] && break
  sleep 5
done
atrium admin photos list --collection recent --limit 5
```

Record: per batch, the copy-completion time and the time the 100th file became
`ready`; the scan interval and stability settings in force.

## 6. First index — progress throughout, dashboard never blocked

Point a source at the full library and start with an empty database.

```bash
atrium admin sources scan family_photos --full
watch -n 10 'atrium admin diag --json | python3 -c "import json,sys;d=json.load(sys.stdin);print(d[\"photos\"],d[\"jobs\"])"'
# In parallel, prove the dashboard is unaffected:
./scripts/bench-api.sh 200 bench-api-during-index.txt
```

Record: total wall time for the whole library, files per second, peak
`jobs.queued`, and the `/home` P95 measured **during** the index. This run sets
the first-index budget; there is no promised figure before it.

## 7. TV reconnect — within 60 s

At least 10 network interruptions. Disconnect the TV's network (or the AP) for
two minutes, restore it, and time from both the network and Core being
reachable to the dashboard being live again.

```bash
atrium admin screens list      # online flips false, then true
```

Record: 10 samples, and whether the TV showed cached content or the connect
screen while offline.

## 8. NAS recovery — within 120 s

At least 5 unmount/remount cycles, timed from the mount being available and its
identity verified.

```bash
diskutil unmount /Volumes/photos
atrium admin sources list        # offline / root_missing within one probe (15 s)
# remount, then:
atrium admin sources list        # online, identity bound
atrium admin scans list --source family_photos
```

Record: 5 samples of offline-detection time and of return-to-online time, and
confirmation that no photo was removed during the outage.

## 9. Core recovery — dashboard within 120 s

Three process kills and three system reboots.

```bash
pkill -f 'atrium serve'          # launchd restarts it (ThrottleInterval 5)
time curl -sf --cacert "$ATRIUM_CA" "$ATRIUM_URL/health/ready"
atrium admin commands list --status unknown   # in-flight commands, honestly reported
```

Record: 3 kill samples and 3 reboot samples, timed from macOS being able to run
services and the configuration being readable. Record disk-unlock time
separately — FileVault is a deployment fact, not an Atrium latency.

## 10. Seven-day availability — ≥ 99.5%, no unrecovered crash

Probe every 10 s for seven days and keep the raw log.

```bash
while :; do
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "$(curl -o /dev/null -s -w '%{http_code}' --cacert "$ATRIUM_CA" "$ATRIUM_URL/health/live" || echo 000)"
  sleep 10
done | tee uptime-7d.log
```

Record: the availability percentage, every gap with its cause, and the restart
count from the log. Fault-injection windows are recorded separately and are not
netted out of the normal-operation figure.

## 11. TV stability — 24 h unattended, then daily windows

V0.1 asks for 24 hours with no intervention; V1 asks for seven days of the
household's real display windows. No white screen, no freeze attributable to
the app.

Record: start and end timestamps, any manual intervention (which ends the run),
and hardware sleep or user switching separately.

## 12. Resource budget

```bash
ps -o pid,rss,%cpu,command -p "$(pgrep -f 'atrium serve')"
atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["cache"])'
df -h "$ATRIUM_DATA_DIR"
```

Sample after warm-up and again after the seven-day run. Record RSS at both
points (the question is whether it grows without bound, not its absolute
value), cache bytes against the 10 GB budget, and confirm that low disk paused
new preview work rather than degrading the API.

## 13. Failure matrix

Every row of PRD §7.2 must be exercised with a real fault. See
`docs/ops/failure-matrix.md` for how to inject each one and what to observe.

## 14. Backup restore drill — under 60 minutes

```bash
atrium backup now
atrium backup list
launchctl bootout "gui/$UID/com.atrium.core"
atrium restore --from /Volumes/backup/atrium/atrium-<stamp>.db
launchctl bootstrap "gui/$UID" ~/Library/LaunchAgents/com.atrium.core.plist
atrium admin diag                # screens and sources are back
atrium admin screens list        # the TV reconnects with its existing pairing
```

Record: elapsed time to configuration and basic display, and separately the
time for previews to regenerate — the cache is not backed up, so a full library
restore is a longer, separate number.

## Security check (every acceptance run)

```bash
grep -c 'atr_scr_\|atr_adm_\|Authorization\|atrium_screen' \
  "$ATRIUM_DATA_DIR/logs/atrium.log"                        # expect 0
grep -c '/Volumes/photos' "$ATRIUM_DATA_DIR/logs/atrium.log" # expect 0 at info+
atrium admin diag --json | grep -c 'atr_\|rel_path\|root_path'  # expect 0
```

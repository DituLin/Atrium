# Failure matrix

Every row of PRD §7.2, how to inject it on real hardware, and what must happen.
These are real steps — unmount a share, revoke a permission, stop a process —
not test doubles: `source.FakeFS` covers the same scenarios in the Go tests, and
this page is what proves the same behaviour on the Mac mini.

Run each row, record pass or fail with the observed timing in
`docs/ops/g0-record.md`, and keep the log excerpt for anything that failed.

Baseline before every row:

```bash
atrium admin diag --json > diag-before.json
```

## Safety

Only two rows write anything: photo/source revocation (reversible with
`include` / `restore`) and the cache-full row (deletes only regenerable
previews). Nothing here writes to the NAS — Atrium opens it read-only — and no
row asks you to delete an original.

---

## 1. Internet down

**Inject.** Unplug the WAN uplink at the router, or block it in the firewall.
Leave the LAN intact.

**Expect.** Photos, screen control and the clock keep working. The weather
widget goes `stale` and then hides; nothing else changes.

```bash
atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["widgets"])'
atrium admin screen refresh living_room_tv --wait     # still applied
```

**Verification point.** No CDN and no cloud login is needed to keep the basics
running.

---

## 2. TV network drops for 5 minutes

**Inject.** Disconnect the TV from Wi-Fi, or power down its AP, for 5 minutes.

**Expect.** The TV keeps showing what it had cached and displays a connection
notice. `screens[].online` flips to false within 45 s. Nothing queues up: a
command issued during the outage is `409 screen_offline`. On reconnect the TV
re-authenticates and pulls a fresh snapshot within 60 s, with no manual
refresh. A TV with nothing cached shows the connect screen instead.

```bash
atrium admin screens list                             # online: no
atrium admin screen navigate living_room_tv --route dashboard   # 409 screen_offline
# after reconnect
atrium admin screens list                             # online: yes
atrium admin commands list --limit 10                 # no backlog replayed
```

---

## 3. Core killed and restarted

**Inject.** Issue a command and kill the process before the TV acknowledges it.

```bash
atrium admin screen navigate living_room_tv --route photos &
pkill -f 'atrium serve'
```

**Expect.** launchd restarts it (`ThrottleInterval 5`). The pairing and the
configuration survive. The in-flight command becomes `unknown` with
`error_code: server_restart` — never `applied` and never `failed`.

```bash
atrium admin commands list --status unknown --limit 5
atrium admin screens list        # the TV reconnects on its own
```

**Verification point.** An unconfirmed command is never reported as success.

---

## 4. Mac mini reboots

**Inject.** `sudo reboot`.

**Expect.** Behaviour depends on the deployment choice recorded in
`g0-record.md`: with auto-login and FileVault off it returns unattended; with
FileVault on it waits for a person to unlock the disk, and the NAS mount agent
runs only after login. Atrium must not be the reason for a delay.

```bash
launchctl print "gui/$UID/com.atrium.core" | head -20
atrium admin sources list        # mount present before the first scan
```

Record disk-unlock time and mount time separately from Atrium's own start.

---

## 5. NAS unmounted, permission denied, or reads hang

**Inject, one at a time.**

```bash
# a) unmount
diskutil unmount force /Volumes/photos

# b) permission denied — revoke read on the NAS side for the atrium-ro account,
#    or locally on a test tree:
chmod 000 /Volumes/photos/family/2019

# c) hang — pause the SMB client so syscalls block without returning:
sudo pkill -STOP -f 'mount_smbfs|smbfs'     # resume with -CONT
```

**Expect.**

| Fault | Health | Behaviour |
| --- | --- | --- |
| unmounted | `offline` / `root_missing` within one 15 s probe | No removals. The scan is recorded `failed` and the generation does **not** advance. Cached previews keep serving. |
| permission denied | `degraded` / `permission_denied` | The unreadable subtree is counted in `scan_runs.errors` and skipped; the rest of the library keeps indexing. |
| hung reads | `unknown` / `probe_timeout`, `stuck_ops` rising | Calls return `ErrStuck` at `io_timeout`; the API and the WebSocket heartbeat stay responsive. |

```bash
atrium admin sources list
atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["sources"])'
curl -o /dev/null -s -w '%{time_total}\n' --cacert "$ATRIUM_CA" \
  -H "Authorization: Bearer $ATRIUM_ADMIN_TOKEN" "$ATRIUM_URL/api/v1/home"   # still fast
atrium admin screen refresh living_room_tv --wait                            # still applied
```

**Verification point.** No bulk index deletion, an empty mount point is never
taken for the library, and NAS I/O never blocks the API or the heartbeat.
Remember to `chmod 755` and `pkill -CONT` afterwards.

**Empty-mount trap.** After unmounting, `/Volumes/photos` may still exist as an
empty local directory. With `require_mount: true` the root is on `apfs`, so
health is `unknown` / `not_a_mount` and the scan refuses to run. Confirm this
explicitly — it is the check that stops a lost mount from emptying the index.

---

## 6. Photo mid-copy, corrupt, oversized or unsupported

**Inject.**

```bash
# mid-copy: write slowly into the authorized root
dd if=/dev/urandom bs=1m count=40 | pv -L 200k > /Volumes/photos/family/slow.jpg

# corrupt: valid header, garbage body
head -c 64 good.jpg > /Volumes/photos/family/corrupt.jpg
head -c 200000 /dev/urandom >> /Volumes/photos/family/corrupt.jpg

# oversized: dimensions beyond storage.max_pixels
# unsupported: a .heic when heic.converter is off, or a .txt renamed .jpg
```

**Expect.** The mid-copy file is not indexed until it stops changing. The
corrupt one gets `preview_error: decode_failed` and retries with backoff, then
stops — no infinite retry. The oversized one is `too_large`. The unsupported
one lands in `unsupported`. The slideshow keeps running through all of it.

```bash
atrium admin photos get 01JPHOTOID       # preview_status and preview_error
atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["photos"])'
```

---

## 7. Empty library, first import, midnight crossing

**Inject.** Point a source at an empty directory; then at a full library from
an empty database; then leave a TV running across local midnight.

**Expect.** An empty library shows an empty state, not an error. The first
import reports `baseline: importing` with live progress and everything it finds
is `is_baseline`, so `recent` stays empty. At midnight, `captured_today` and
`new_today` roll over in the **home** timezone. A photo with no capture time
never appears in `captured_today`; it is counted in `unknown_captured`.

```bash
atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["photos"])'
curl -s --cacert "$ATRIUM_CA" -H "Authorization: Bearer $ATRIUM_ADMIN_TOKEN" \
  "$ATRIUM_URL/api/v1/photos?collection=captured_today" | head -c 400
```

---

## 8. Duplicate, out-of-order, expired or unacknowledged commands

**Inject.**

```bash
# Back-to-back commands: the older one must lose.
atrium admin screen navigate living_room_tv --route photos &
atrium admin screen navigate living_room_tv --route dashboard &
wait

# Unacknowledged: stop the TV client (or pull its network) right after issuing.
atrium admin screen show living_room_tv --photo 01JPHOTOID --wait
```

**Expect.** A duplicate `command_id` executes once. A command with a sequence
lower than what the screen has applied is acked `failed / superseded`. A
command never delivered by its 10 s TTL becomes `expired`. One delivered
without an acknowledgement becomes `unknown` about 12 s later, and the next
heartbeat attaches `result.observed` with the sequence and route the screen
reports. The final screen state and the recorded result agree.

```bash
atrium admin commands list --screen living_room_tv --limit 10
atrium admin commands get 01JCOMMANDID     # prints "screen reports sequence N on route X"
```

---

## 9. Unpaired access, revoked device, out-of-scope resource ID

**Inject.**

```bash
curl -i --cacert "$ATRIUM_CA" "$ATRIUM_URL/api/v1/home"                       # no credential
curl -i --cacert "$ATRIUM_CA" -H "Authorization: Bearer atr_scr_wrong" \
  "$ATRIUM_URL/api/v1/photos"                                                  # bad credential
curl -i --cacert "$ATRIUM_CA" -H "Authorization: Bearer $SCREEN_TOKEN" \
  "$ATRIUM_URL/api/v1/screens"                                                 # wrong scope
curl -i --cacert "$ATRIUM_CA" -H "Authorization: Bearer $SCREEN_TOKEN" \
  "$ATRIUM_URL/api/v1/media/photos/01JNOTMINE?variant=preview"                 # unknown ID
atrium admin screens revoke living_room_tv                                     # while it is live
```

**Expect.** `401` without a credential, `403` for the wrong scope, `404` for an
unknown or unauthorized photo ID — indistinguishable from a real one, so
nothing is enumerable. Ten failures a minute from one IP earn a `429` for 60 s.
Revocation closes the live session with close code `4002` immediately and the
TV clears its cache and returns to pairing. Nothing in the log or in
diagnostics carries a credential or a path.

```bash
grep -c 'atr_scr_\|atr_adm_\|Authorization' "$ATRIUM_DATA_DIR/logs/atrium.log"   # 0
atrium admin diag --json | grep -c 'rel_path\|root_path\|atr_'                   # 0
```

---

## 10. Photo or source revoked, then rescanned

**Inject.**

```bash
atrium admin photos exclude 01JPHOTOID --reason "acceptance"
atrium admin sources revoke family_photos
atrium admin sources scan family_photos --full     # refused while revoked
atrium admin sources restore family_photos
atrium admin sources scan family_photos --full
```

**Expect.** An excluded photo returns `404` on the media route and stays
excluded across a full rescan, because the exclusion is stored as a rule and
not as a photo-row flag. A revoked source hides all of its photos while keeping
the index; restoring brings them back without a re-import. An online screen
showing a now-revoked photo drops it.

```bash
atrium admin exclusions list
curl -o /dev/null -s -w '%{http_code}\n' --cacert "$ATRIUM_CA" \
  -H "Authorization: Bearer $ATRIUM_ADMIN_TOKEN" \
  "$ATRIUM_URL/api/v1/media/photos/01JPHOTOID?variant=preview"     # 404
```

---

## 11. TV offline for more than 24 hours

**Inject.** Disconnect the TV from the network and leave it displaying for more
than 24 hours (a shortened `screens.offline_after` does **not** substitute:
this rule is a client-side cache limit, not a presence timeout).

**Expect.** After 24 hours without a successful authorization check the client
stops showing protected photos, clears its memory and disk cache, and switches
to the connect page — including whatever was already rendered. On reconnect it
re-validates before showing anything again.

**Verification point.** Covering the current frame matters as much as deleting
the file: a stale photo left on screen is the failure.

---

## 12. One photo held across slideshow cycles

**Inject.**

```bash
atrium admin screen show living_room_tv --photo 01JPHOTOID --wait
# wait through at least two slideshow_interval periods (2 x 30 s by default)
atrium admin screen refresh living_room_tv --wait
```

**Expect.** The photo stays on screen across both cycles. `refresh` re-reads
data and keeps both the page and the paused state; it does not resume the
slideshow. Only a user action or a new valid `navigate` / `show` changes it.

---

## 13. Cache full or disk low

**Inject.** Lower the budget so the janitor has work, then fill the volume.

```yaml
storage:
  cache_budget_bytes: 209715200   # 200 MB, temporarily
  min_free_bytes: 5368709120      # 5 GB
```

```bash
# Consume free space on the data volume (delete it afterwards).
mkfile 20g "$ATRIUM_DATA_DIR/ballast"
```

**Expect.** The janitor evicts by least-recent access down to 90% of the
budget, marking rows `preview_status: evicted` while keeping the index. Below
`min_free_bytes` new preview work pauses with
`cache.paused_reason: low_disk` and the API keeps answering. Configuration and
the index are never treated as cache.

```bash
atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["cache"])'
rm "$ATRIUM_DATA_DIR/ballast"
```

---

## 14. Small cache budget evicts previews

**Inject.** Keep the reduced budget from row 13 with plenty of free disk, and
browse enough photos to force eviction.

**Expect.** An evicted preview returns `202` with `Retry-After: 5` while it is
rebuilt, and rebuild jobs deduplicate rather than piling up. While the source
is offline the same request is `503 preview_unavailable`, not a permanent
failure — the photo is never excluded because of a cache state.

```bash
curl -i -o /dev/null -s -w '%{http_code}\n' --cacert "$ATRIUM_CA" \
  -H "Authorization: Bearer $ATRIUM_ADMIN_TOKEN" \
  "$ATRIUM_URL/api/v1/media/photos/01JEVICTED?variant=preview"   # 202, then 200
atrium admin diag --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["jobs"])'
```

Restore the real `cache_budget_bytes` and `min_free_bytes` afterwards and
record that you did.

---

## On fault injection built into the binary

There is none, and that is deliberate. Every row above is injected from
outside the process — a mount, a permission, a signal, a full disk — so what is
exercised is the shipping code path, not a debug branch. The equivalent
scenarios are covered in the Go tests through `source.FakeFS` (hang, EACCES,
disappear, slow) and fake disk statistics, which is where a regression is
caught cheaply. If a future release needs an in-binary fault switch, it must sit
behind a build tag or an explicitly documented config key that is off by
default, and it must never ship enabled in a release binary.

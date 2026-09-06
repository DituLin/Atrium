# Atrium runbook

Everything an operator does on the Mac mini, in the order it is normally done.
`deploy/README.md` covers the launchd mechanics in more depth; this page is the
task list. Command examples use `192.168.1.10`, `/Volumes/photos/family` and
`living_room_tv` as placeholders — substitute your own and never commit the
real ones.

## Conventions

```bash
export ATRIUM_CONFIG="$HOME/.config/atrium/config.yaml"
export ATRIUM_DATA_DIR="$HOME/Library/Application Support/Atrium"
```

The admin token lives at `$ATRIUM_DATA_DIR/admin.token` (mode 0600) and the CLI
finds it, and the local CA, on its own. Logs are JSON lines at
`$ATRIUM_DATA_DIR/logs/atrium.log`, rotated at 20 MB with seven days of
retention; launchd's own stdout and stderr land beside them.

## 1. Install

```bash
make release
sudo cp bin/atrium-darwin-arm64 /usr/local/bin/atrium
atrium init --config "$ATRIUM_CONFIG" --public-url https://192.168.1.10:8443
deploy/launchd/install.sh --config "$ATRIUM_CONFIG"
```

`atrium init` writes the configuration if it is missing, creates the 0700 data
tree, generates the local CA and server certificate, and prints the first admin
token. Copy that token now; it is stored only as a hash.

Check it came up:

```bash
curl --cacert "$ATRIUM_DATA_DIR/tls/ca.crt" https://127.0.0.1:8443/health/ready
atrium admin diag
```

## 2. Mount the NAS share read-only

Atrium never stores NAS credentials and never mounts anything itself. The share
must already be mounted in the same login session the LaunchAgent runs in, and
the account it uses must be read-only on the NAS side.

Store the password in the login keychain once, interactively:

```bash
security add-internet-password \
  -a atrium-ro -s nas.local -r "smb " -l "Atrium NAS (read-only)" -w
```

Then mount it from a per-user LaunchAgent that reads the password back from the
keychain. No password ever appears in a file, in the process table of another
user, or in this repository.

`~/Library/LaunchAgents/com.atrium.mount.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.atrium.mount</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/sh</string>
    <string>-c</string>
    <string>/usr/local/bin/atrium-mount-nas.sh</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key>
  <dict><key>SuccessfulExit</key><false/></dict>
  <key>ThrottleInterval</key><integer>30</integer>
</dict>
</plist>
```

`/usr/local/bin/atrium-mount-nas.sh`:

```bash
#!/bin/sh
set -eu
MOUNT_POINT="/Volumes/photos"
SHARE="nas.local/photos"
ACCOUNT="atrium-ro"

# Already mounted: nothing to do.
if /sbin/mount | grep -q " on ${MOUNT_POINT} "; then
  exit 0
fi

mkdir -p "${MOUNT_POINT}"
# -N: never prompt; take the password from the login keychain item created
# above (or by Finder's "Remember this password in my keychain"). The script
# never sees the password. -o rdonly is defence in depth: the NAS account is
# read-only too.
exec /sbin/mount_smbfs -N -o rdonly "//${ACCOUNT}@${SHARE}" "${MOUNT_POINT}"
```

Two things the first real deployment taught us (G0, 2026-09-06):

- **The mount must be created in the GUI login session.** An SMB mount made
  from an SSH session belongs to that session's audit context; the LaunchAgent
  (which runs in the console user's `gui/<uid>` domain) can `stat` the root but
  every deeper read blocks on a hidden re-authentication prompt, and Atrium
  reports the source as `degraded / stuck_io`. Mount it from a `gui/<uid>`
  LaunchAgent as above, from a Login Item, or interactively in Finder — never
  from a remote shell.
- **macOS asks once for "Network Volumes" access.** The first time the
  `atrium` binary reads a network volume, Transparency, Consent and Control
  (TCC) shows a prompt on the Mac mini's display and blocks the read until it
  is answered. Approve it on the screen (or through screen sharing), or add
  the binary under System Settings → Privacy & Security → Full Disk Access
  before installing the LaunchAgent. Replacing the binary keeps the grant only
  while the path is unchanged.

```bash
chmod 700 /usr/local/bin/atrium-mount-nas.sh
launchctl bootstrap "gui/$UID" ~/Library/LaunchAgents/com.atrium.mount.plist
```

Then point a source at the photo directory inside the share. The authorized
root does not have to be the mount point — `/Volumes/photos/family` is the
normal shape — but with `require_mount: true` it must live on a network
filesystem, so a share that failed to mount leaves a local directory that
Atrium refuses as `not_a_mount` rather than indexing as an empty library.

```yaml
sources:
  - id: family_photos
    name: "Family photos"
    root: "/Volumes/photos/family"
    identity:
      require_mount: true
      marker_file: ".atrium-source"   # optional, extra proof of the right share
```

Verify:

```bash
atrium admin sources list      # health online, identity bound/smbfs
```

## 3. Pair the TV

1. Open `https://192.168.1.10:8443/` on the TV. It shows a six-digit code.
2. On the Mac mini:

```bash
atrium admin pair list
atrium admin pair approve 123456 --id living_room_tv --name "Living room TV"
```

3. The TV claims the credential within its five-minute window and lands on the
   dashboard. The credential is a cookie; it is minted at claim time, so no
   unclaimed token ever exists at rest.

If the TV will not trust the local CA, export it and install it there:

```bash
atrium tls export-ca > atrium-ca.crt
```

Record the trust method in `docs/ops/g0-record.md`.

## 4. Scanning

Scans run every `scan.interval` (60 s by default), serially per source, and
never overlap. To force one:

```bash
atrium admin sources scan family_photos            # incremental
atrium admin sources scan family_photos --full     # re-derive everything
atrium admin scans list --source family_photos
atrium admin scans get 01JSCANID
```

The first pass is a **baseline import**: everything it finds is marked
`is_baseline` and is deliberately absent from the `recent` collection, so an
archive imported today does not read as "500 photos added today".

A file is indexed only after it stops changing (`stability_checks` re-stats
across `stability_interval`), so a copy in progress is never half-imported. A
file that disappears is removed only after **two** completed scans miss it, and
a scan that failed or aborted never advances that counter.

## 5. Control the screen

See `docs/ops/control.md`. The short version:

```bash
atrium admin screens list
atrium admin screen navigate living_room_tv --route photos --collection recent --wait
atrium admin screen show living_room_tv --photo 01JPHOTOID --wait
atrium admin screen refresh living_room_tv --wait
atrium admin commands list --status unknown
```

## 6. Diagnose

```bash
atrium admin diag              # tables
atrium admin diag --json       # the whole §6.11 document
```

Read it top down: `sources` first (a share problem explains most symptoms),
then `jobs` and `cache` (a backlog or a paused cache explains missing
previews), then `screens` and `commands`, then `recent_errors`, which is the
last 100 warnings and errors by component and code.

The document contains no credentials and no NAS paths by design. To see the
path behind one photo, ask for it explicitly:

```bash
atrium admin photos get 01JPHOTOID   # the only route that reveals rel_path
```

Common readings:

| Symptom | Look at | Likely cause |
| --- | --- | --- |
| TV shows the connect screen | `screens[].online`, then the TV's own network | Session lost; the client reconnects with backoff up to 30 s |
| No new photos | `sources[].last_scan`, `jobs.queued` | Share offline, scan failing, or a preview backlog |
| Photos exist but do not display | `photos.preview_failed`, `cache.paused_reason` | Decode failures, or the cache paused on low disk |
| Commands come back `unknown` | `commands.last_24h`, `screens[].applied_sequence` | The TV is receiving but not acknowledging: check the client, not the server |
| `health: unknown`, detail `identity_mismatch` | `sources[]` | A different share is mounted at the root; nothing is scanned or removed until it is resolved |

To accept a legitimately changed mount (a NAS rebuild, a new share name):

```bash
atrium admin sources rebind-identity family_photos
```

## 7. Backup and restore

Backups are a `VACUUM INTO` snapshot plus a copy of the configuration. The
cache is never backed up; previews regenerate on demand.

```yaml
backup:
  enabled: true
  dir: "/Volumes/backup/atrium"   # must be outside data_dir and every source root
  time: "03:00"                   # home time
  keep: 7
```

```bash
atrium backup now
atrium backup list
```

Restoring replaces the live database, so the service must be stopped. Atrium
holds an advisory lock on `atrium.lock` while it runs and `restore` refuses
while that lock is held.

```bash
launchctl bootout "gui/$UID/com.atrium.core"
atrium restore --from /Volumes/backup/atrium/atrium-20260906-030000.db
launchctl bootstrap "gui/$UID" ~/Library/LaunchAgents/com.atrium.core.plist
```

The previous database is moved aside as `atrium.db.pre-restore-<timestamp>`,
never deleted. Verify with `atrium admin diag` that the screen and source rows
are the ones you expect before deleting it.

A restore drill is part of acceptance: see `docs/ops/acceptance.md`.

## 8. Rotate the admin token

```bash
atrium admin token rotate
```

A new token is issued and printed once, the token file is rewritten, and the
previous token is revoked **after** the response is written, so a dropped
response cannot lock you out. If it does anyway, recover offline:

```bash
launchctl bootout "gui/$UID/com.atrium.core"
atrium token reset          # revokes every admin token and issues one
```

## 9. Upgrade and rollback

```bash
atrium backup now                                   # always first
make release
sudo cp bin/atrium-darwin-arm64 /usr/local/bin/atrium
launchctl kickstart -k "gui/$UID/com.atrium.core"
atrium admin diag                                   # version, migrations
```

Migrations run forward only. To roll back, restore the **matching** backup
alongside the previous binary; never run an older binary against a newer
schema.

```bash
launchctl bootout "gui/$UID/com.atrium.core"
sudo cp /usr/local/bin/atrium.previous /usr/local/bin/atrium
atrium restore --from /Volumes/backup/atrium/atrium-<before-the-upgrade>.db
launchctl bootstrap "gui/$UID" ~/Library/LaunchAgents/com.atrium.core.plist
```

Keep the previous binary as `/usr/local/bin/atrium.previous` at upgrade time so
this is a copy, not a rebuild.

## 10. Log locations

| What | Where |
| --- | --- |
| Application log (JSON lines) | `$ATRIUM_DATA_DIR/logs/atrium.log` |
| launchd stdout/stderr | `$ATRIUM_DATA_DIR/logs/atrium.out.log`, `atrium.err.log` |
| Rotation | 20 MB per file, `logging.retain_days` (7), `logging.max_total_mb` (200) |

Tokens, cookies, `Authorization` headers, NAS root paths, relative paths and
mount sources are never logged at `info` or above. `debug` may include
`rel_path` and is off by default; turn it on only while investigating, and turn
it off afterwards.

```bash
grep -c 'atr_scr_\|atr_adm_\|Authorization' "$ATRIUM_DATA_DIR/logs/atrium.log"   # expect 0
```

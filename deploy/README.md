# Deploying Atrium on macOS

Atrium runs as a single binary supervised by `launchd`. This directory holds the
LaunchAgent template and its install and uninstall scripts.

## Why a LaunchAgent and not a LaunchDaemon

A LaunchAgent starts after the user logs in and runs inside that user's GUI
session. That matters because an SMB or AFP share mounted by a user is only
visible inside that session: a LaunchDaemon starting before login would find no
`/Volumes/...` mount at all.

The trade-off is that the Mac mini must reach a logged-in session after a
reboot. The options, in the order most home setups pick them:

| Option | Behaviour after a power cut | Notes |
| --- | --- | --- |
| Auto-login, FileVault off | Comes back unattended | Least secure at rest; acceptable when the machine is physically safe and holds no other data |
| FileVault on, no auto-login | Waits for someone to unlock the disk | Safest; Atrium stays down until a person logs in |
| FileVault on with `sudo fdesetup authrestart` | Comes back after a planned reboot only | Good for maintenance, not for a power cut |
| LaunchDaemon with an `autofs` mount | Starts before login | Requires `/etc/auto_master` and a stored credential; documented below but not the default |

Record the chosen strategy in `docs/ops/g0-record.md`.

## Install

```
make build
sudo cp bin/atrium /usr/local/bin/atrium
atrium init --config ~/.config/atrium/config.yaml
deploy/launchd/install.sh --config ~/.config/atrium/config.yaml
```

The script substitutes the paths into `com.atrium.core.plist.tmpl`, validates
the result with `plutil -lint`, writes
`~/Library/LaunchAgents/com.atrium.core.plist`, and bootstraps it into the
`gui/$UID` domain. Re-running it is safe: the previous service is booted out
first.

Flags and their environment equivalents:

| Flag | Variable | Default |
| --- | --- | --- |
| `--binary` | `ATRIUM_BINARY` | `/usr/local/bin/atrium` |
| `--config` | `ATRIUM_CONFIG` | `~/.config/atrium/config.yaml` |
| `--data-dir` | `ATRIUM_DATA_DIR` | `~/Library/Application Support/Atrium` |
| `--label` | `ATRIUM_LABEL` | `com.atrium.core` |

## Verify

```
launchctl print gui/$UID/com.atrium.core | head -20
curl -sk https://127.0.0.1:8443/health/live
atrium admin diag --config ~/.config/atrium/config.yaml
```

## Logs

| File | Content |
| --- | --- |
| `<data_dir>/logs/atrium.log` | Application log, JSON lines, rotated at 20 MB, kept 7 days |
| `<data_dir>/logs/launchd.out.log` | Anything the process writes to stdout |
| `<data_dir>/logs/launchd.err.log` | Startup failures before the logger exists |

If the service restarts in a loop, `launchd.err.log` holds the reason; a config
validation failure is printed there in full with paths redacted.

## Mounting the NAS share read-only

Atrium never stores NAS credentials and never writes to a source root. Mount the
share yourself with an account that has read-only rights.

**Login item (simplest).** Create a small script that runs `mount_smbfs` (or use
Finder's "Connect to Server" with "Remember this password in my keychain") and
add it to System Settings → General → Login Items. The mount then exists in the
same session as the LaunchAgent.

**autofs (survives sleep better).** Add to `/etc/auto_master`:

```
/-      auto_atrium     -nosuid
```

and in `/etc/auto_atrium`:

```
/Volumes/photos -fstype=smbfs,ro ://readonly_user@nas/photos
```

Then `sudo automount -vc`. The password comes from the system keychain, not from
Atrium. Set `identity.require_mount: true` in the config so Atrium refuses to
scan when the share is not mounted, which is what stops an empty mount point
from looking like "every photo was deleted".

## Upgrade and rollback

```
atrium backup now                       # once backups land in H1
sudo cp bin/atrium /usr/local/bin/atrium
launchctl kickstart -k gui/$UID/com.atrium.core
```

Migrations run forward only. To roll back, restore the previous binary together
with the backup taken before the upgrade.

## Uninstall

```
deploy/launchd/uninstall.sh
```

The data directory, including the database, certificates and the admin token, is
left in place. Remove it by hand if you really want to start over.

# G0 hardware verification record

Template for the maintainer-executed hardware gate (dev plan §7). Fill it in on
the real Mac mini, NAS and TV; leave a field blank rather than guessing, and
mark anything not attempted as `not run`.

**Do not commit real IP addresses, hostnames, share names, account names,
paths, serial numbers or photo file names.** Describe them
(`"static IPv4 on the LAN"`, `"SMB share on the NAS"`) or use the repository
placeholders `192.168.1.10`, `/Volumes/photos/family`, `living_room_tv`. A
completed record is an operations document, not a public one.

| | |
| --- | --- |
| Date started | 2026-09-06 |
| Date completed | partial (Mac mini + NAS done; TV not run) |
| Executed by | maintainer with Claude Code |
| Atrium build (`atrium version`) | 0.1.0 (pre-release build from the V1 base tree) |
| Web bundle version | 0.1.0, 64 KB gzip JS |

---

## 1. Mac mini

| Field | Value |
| --- | --- |
| Model and year | Mac mini (2024) |
| Chip | Apple M4 |
| RAM | 32 GB |
| macOS version | 15.2 |
| Free disk on the data volume | 48 GB before the first index; 39 GB with 7.4k previews cached |
| Data directory location (describe, do not paste) | a directory under the service user's home |
| FileVault enabled | yes |
| Auto-login enabled | console user was logged in during G0; auto-login not verified |
| Sleep settings (`pmset -g`) | sleep 0 (never), displaysleep 10, disksleep 10, womp 1 |
| Power-loss recovery setting | autorestart 0 (not enabled) |
| Launch strategy chosen (LaunchAgent / LaunchDaemon) | LaunchAgent (`gui/<uid>`) |
| Reason for that choice | SMB share is mounted in the user session; no passwordless sudo for autofs |
| Time from power-on to Atrium answering `/health/ready` | not run |
| Of which, disk unlock | not run |

## 2. NAS and the share

| Field | Value |
| --- | --- |
| Protocol (SMB / NFS / AFP) | SMB (smbfs) |
| Read-only account used (describe, do not name) | the owner's personal NAS account, mounted `-o rdonly`; a dedicated read-only account is still recommended |
| Mount method (login agent + Keychain / autofs / other) | Finder "Connect to Server" in the console session with the password remembered in the login keychain; an earlier `mount_smbfs` from a remote shell was unusable by the LaunchAgent (open issue 1) |
| Mount point shape (`/Volumes/<share>`) | `/Volumes/<share>` (Finder default); note Finder mounts read-write at the client, Atrium itself only reads |
| Authorized root: mount point itself or a subdirectory? | a subdirectory of the share (`<share>/Photos`) |
| Filesystem reported by `atrium admin sources list` | `bound/smbfs` |
| `marker_file` in use | no |
| Directory count | 426 |
| File count | 17,172 stills indexed (12,376 jpg, 2,193 jpeg, 2,119 heic, 484 png); ~2.4k videos ignored by the allowlist |
| Total size | ≈ 65 GB of stills (avg 3.8 MB) |
| JPEG / PNG / HEIC / other ratio | 85 % / 3 % / 12 % / <1 % |
| Largest file | 36 MB |
| Files with no EXIF capture time (`unknown_captured`) | 540 (3 %); 7,746 exact, 8,886 inferred (no timezone tag) |

`atrium admin sources list` after the first scan (redact the root path):

```
```

`atrium admin diag --json` excerpt, `sources` and `photos` sections:

```json
```

### First index

| Field | Value |
| --- | --- |
| Wall time for the full library | walk 18 s; metadata ≈ 27 files/s (single worker, before the SQLite fix); previews ≈ 4.4 photos/s with 2 workers → ≈ 65 min projected for 17k |
| Files per second | ≈ 950 files/s for the metadata walk over SMB |
| Peak `jobs.queued` | 17,105 |
| `/home` P95 measured during the index | not measured formally; `/home` answered in ≈ 40 ms while the source was offline |
| Scan duration vs the 60 s interval | 18–20 s for 17k files; 11 scheduled scans completed without overlap |
| Partitioned scanning needed? (design §13) | no |

## 3. TV

| Field | Value |
| --- | --- |
| Model and year | not run (TV) |
| System / OS version | |
| Browser or WebView and version | |
| Viewport (CSS pixels) and device pixel ratio | |
| Full-screen behaviour | |
| Cookie persistence across a power cycle | |
| WebSocket support | |
| `Intl` timezone support | |
| Local CA trust method (or why it was impossible) | |
| Back-key code observed | |
| Standby / resume behaviour | |
| 24 h unattended run result | |
| Seven-day daily-window result | |

## 4. Network

| Field | Value |
| --- | --- |
| Mac mini link (wired / Wi-Fi, band) | |
| TV link (wired / Wi-Fi, band) | |
| Benchmark client and its link | |
| Router / AP model | |
| Any VLAN or client isolation | |

## 5. Performance measurements

Method and thresholds: `docs/ops/acceptance.md`. Attach the raw
`bench-api-*.txt` and `bench-commands-*.txt` files.

| Metric | Target | Measured | Samples | Pass |
| --- | --- | --- | --- | --- |
| `GET /home` P95 | ≤ 200 ms | | 1000 | |
| `GET /photos?collection=random` P95 | ≤ 200 ms | | 1000 | |
| `GET /nas/status` P95 | ≤ 200 ms | | 1000 | |
| Read API error rate | < 1% | | | |
| Initial dashboard P95 | ≤ 3 s | | ≥ 20 | |
| Screen control P95 | ≤ 1 s | | ≥ 100 | |
| Screen control max | ≤ 3 s | | | |
| Data push P95 | ≤ 2 s | | ≥ 100 | |
| New photo visible P95 | ≤ 120 s | | ≥ 5 batches | |
| TV reconnect | ≤ 60 s | | ≥ 10 | |
| NAS recovery | ≤ 120 s | | ≥ 5 | |
| Core recovery (process kill) | ≤ 120 s | < 8 s (LaunchAgent KeepAlive) | 2 | pass (informal) |
| Core recovery (system reboot) | ≤ 120 s | | 3 | |
| Seven-day availability | ≥ 99.5% | | 10 s probe | |
| Unrecovered crashes | 0 | | | |

### Resource budget

| Field | After warm-up | After seven days |
| --- | --- | --- |
| Core RSS | | |
| Worker RSS (peak) | | |
| CPU at idle | | |
| Cache bytes / budget | | |
| Free disk on the data volume | | |
| TV memory (if observable) | | |

## 6. Failure matrix

One row per PRD §7.2 scenario; procedure in `docs/ops/failure-matrix.md`.

| # | Scenario | Observed | Timing | Pass |
| --- | --- | --- | --- | --- |
| 1 | Internet down | | | |
| 2 | TV network drops 5 min | | | |
| 3 | Core killed and restarted | | | |
| 4 | Mac mini reboots | | | |
| 5a | NAS unmounted | | | |
| 5b | Permission denied | | | |
| 5c | Reads hang | | | |
| 5d | Empty mount point not indexed | | | |
| 6 | Mid-copy / corrupt / oversized / unsupported | | | |
| 7 | Empty library, first import, midnight | | | |
| 8 | Duplicate / stale / expired / unacknowledged commands | | | |
| 9 | Unpaired, revoked, out-of-scope access | | | |
| 10 | Photo or source revoked, then rescanned | | | |
| 11 | TV offline > 24 h | | | |
| 12 | One photo held across cycles | | | |
| 13 | Cache full / disk low | | | |
| 14 | Small budget evicts previews | | | |

## 7. Backup and restore drill

| Field | Value |
| --- | --- |
| Backup destination (describe, do not paste) | |
| Snapshot size | |
| Time to take a backup | |
| Time to restore configuration and basic display | |
| Time for previews to regenerate afterwards | |
| Pre-restore database preserved and verified | |
| Backup directory confirmed outside the data dir and every source root | |

## 8. Security spot checks

| Check | Command | Result |
| --- | --- | --- |
| No credential in the log | `grep -c 'atr_scr_\|atr_adm_\|Authorization' logs/atrium.log` | 0 |
| No NAS path in the log at info+ | `grep -c '/Volumes' logs/atrium.log` | 0 (root path never logged) |
| No credential or path in diagnostics | `atrium admin diag --json \| grep -c 'atr_\|rel_path\|root_path'` | |
| Cookie attributes on claim | observed `HttpOnly`, `SameSite=Strict`, `Secure` | confirmed in the local e2e run and on the Mac mini |
| Admin routes reject a cookie | `internal/httpapi/auth_test.go` matrix | pass |
| Media route rejects a path parameter | `internal/httpapi/media_test.go` | pass |
| Auth-failure rate limit trips at 10/min | | |

## 9. Decisions on design §16 open items

| # | Item | Decision | Rationale |
| --- | --- | --- | --- |
| 1 | TV path and engine | | |
| 2 | TV trust of the local CA | | |
| 3 | HEIC share of the library; is HEIC P0? | Yes, P0 | 12 % of the library is HEIC; `sips` produced correct previews for 800+ HEIC files, 3 files unsupported in total |
| 4 | Scan duration vs the 60 s interval; partitioned scanning? | Not needed | 17k-file walk completes in ≈ 18 s |
| 5 | SMB hang behaviour; subprocess worker needed? (design §6.6) | Not needed so far | No kernel-level hangs within the mounting session; the only `stuck_io` came from cross-session access (open issue 1) and the 20 s deadline handled it |
| 6 | macOS boot conditions; LaunchAgent vs LaunchDaemon | LaunchAgent | Needs the console user logged in, the share mounted in that session, and the one-time TCC approval; reboot timing not yet measured |

## 10. Configuration defaults changed

| Key | Default | Value used | Why |
| --- | --- | --- | --- |
| `storage.cache_budget_bytes` | 10 GiB | 20 GiB | 7.4k previews already used 4.9 GB (≈ 660 KB per photo incl. thumb); 17k photos need ≈ 11 GB, and a budget below that would evict continuously |

## 11. Open issues and follow-ups

| # | Issue | Severity | Owner | Status |
| --- | --- | --- | --- | --- |
| 1 | An SMB mount created from a remote (SSH) session is unusable by the LaunchAgent: `stat` works, deeper reads block (`degraded / stuck_io`). The share must be mounted in the GUI login session. | blocking for unattended operation | maintainer | resolved 2026-09-06: mounted via Finder with the keychain; switching the mount changed the mount-from identity and required one `sources rebind-identity`; LaunchAgent then ran with 0 `stuck_io` and survived `kill -9` (back in < 8 s). Remaining: add the share as a Login Item so it remounts after a reboot, and move to a read-only NAS account |
| 2 | macOS TCC "Network Volumes" prompt blocks the first read until approved on screen | high | maintainer | approved once during G0; re-check after binary path changes |
| 3 | Preview jobs that time out with `stuck` consume a retry attempt; a long outage can exhaust the 5 attempts | medium | dev | follow-up: treat `stuck` while the source is `degraded` as a deferral |
| 4 | On a first import previews start only after all metadata jobs (priority 10 vs 5), so the dashboard shows nothing for the first minutes | low | dev | follow-up: interleave priorities or raise preview priority for the first N photos |
| 5 | TV path, CA trust, cookie persistence and the 24 h run are not tested yet | high | maintainer | open |

## 12. Gate decision

| | |
| --- | --- |
| All P0 acceptance rows passed | |
| Blocking issues | |
| Decision (proceed to V1 / rework) | |
| Signed off by / date | |

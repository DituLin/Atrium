# Controlling a screen

Everything on this page runs on the Mac mini that hosts Atrium, over loopback,
with the admin credential. No AI, no cloud service and no TV remote is
involved: FR-18 asks for a reproducible local control and diagnosis path, and
this is it.

## Two ways in

| | Use when |
| --- | --- |
| `atrium admin …` | Day-to-day operation. Resolves the token and the local CA from the data directory automatically, and `--wait` follows a command to its real outcome. |
| `scripts/*.sh` | Copy-paste `curl` examples, for a shell that does not have the binary or for pasting a request into a bug report. |

Both speak the same API. The scripts read `ATRIUM_URL`,
`ATRIUM_ADMIN_TOKEN`, `ATRIUM_CA` and `ATRIUM_DATA_DIR`; when the token
variable is unset they fall back to `admin.token` in the data directory.

```bash
export ATRIUM_URL=https://127.0.0.1:8443
export ATRIUM_DATA_DIR="$HOME/Library/Application Support/Atrium"
# ATRIUM_ADMIN_TOKEN is read from $ATRIUM_DATA_DIR/admin.token when unset.
```

## What the screen is doing right now

```bash
atrium admin screens list
./scripts/screens-list.sh
```

Two fields matter and they mean different things:

- `registered` — the screen was paired and has not been revoked. It survives a
  restart of Atrium and of the TV.
- `online` — a live WebSocket session exists **and** its last heartbeat is
  inside 45 s. It says the Atrium client is running and reachable. It does
  **not** say the panel is lit or that the TV is on the right HDMI input.

## The three commands

`navigate` moves the screen between whitelisted local pages, `show` puts one
authorized photo on screen and pauses the slideshow, `refresh` re-reads data
and re-renders without changing the page or the pause state.

```bash
atrium admin screen navigate living_room_tv --route dashboard
atrium admin screen navigate living_room_tv --route photos --collection recent
atrium admin screen show     living_room_tv --photo 01JPHOTOID
atrium admin screen refresh  living_room_tv
```

```bash
./scripts/screen-navigate.sh living_room_tv photos recent
./scripts/screen-show.sh     living_room_tv 01JPHOTOID
./scripts/screen-refresh.sh  living_room_tv
```

Accepted targets:

| Kind | Payload | Rejected with `400 invalid_command` when |
| --- | --- | --- |
| `navigate` | `route` = `dashboard` \| `photos`, optional `collection` = `recent` \| `captured_today` \| `random` \| `all` | any other route, an unknown collection, or a collection on `dashboard` |
| `show` | `photo_id` | the photo is unknown, removed, excluded, on a revoked source, or has no `ready` preview |
| `refresh` | `{}` | never; stray fields are ignored |

## Accepted is not applied

`POST /commands` returning `202` means the command was recorded and handed to
the session. Only the screen's acknowledgement decides the outcome, so always
read the status back:

```bash
atrium admin screen navigate living_room_tv --route photos --wait
atrium admin commands get 01JCOMMANDID
./scripts/command-get.sh 01JCOMMANDID
```

`--wait` polls every 250 ms for up to 15 s and exits non-zero unless the status
is `applied`, which makes it usable directly in an acceptance script.

| Status | Meaning | What to do |
| --- | --- | --- |
| `accepted` | Issued, not yet resolved. Only visible inside the 10 s TTL. | Wait, or poll. |
| `applied` | The screen rendered the target page or photo. | Nothing. |
| `failed` | The screen refused it (`superseded`, `invalid_route`, `photo_unavailable`, `render_failed`), or the server could not deliver it (`screen_offline`, `delivery_failed`). | Read `error_code`. |
| `expired` | The TTL passed and the command was **never delivered**. It definitely had no effect. | Re-issue. |
| `unknown` | It was delivered but no acknowledgement arrived, or the server restarted while it was open (`server_restart`). The outcome is not knowable. | Look at `result.observed`, which the next heartbeat fills in with the sequence and route the screen reports. Never assume success or failure. |

An `unknown` command is never flipped automatically. `atrium admin commands get`
prints the observed state as `screen reports sequence N on route X`.

## When the screen is offline

An offline screen returns `409 screen_offline` and the body still carries the
command that was recorded as `failed`. Nothing is queued: a TV that comes back
an hour later must not suddenly execute an hour-old navigation (PRD §5.3).

```console
$ atrium admin screen refresh living_room_tv
id           01JCMD…
status       failed (screen_offline)
Error: screen living_room_tv is offline; nothing was queued
```

## Recent history

```bash
atrium admin commands list --screen living_room_tv
atrium admin commands list --status unknown --limit 20
```

Commands are kept for seven days and then pruned by the retention job.

## Revoking a screen

```bash
atrium admin screens revoke living_room_tv
```

The credential stops working immediately, the live session is closed with
close code `4002`, and the pairing history is deleted. The client clears its
cached photos and returns to the pairing page.

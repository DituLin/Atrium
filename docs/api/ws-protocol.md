# WebSocket protocol

This document is the contract between the backend and web tracks for the
realtime channel. It reproduces §9 of
`docs/tech/2026-09/atrium-home-hub/tech-design.md` verbatim; the design remains
the authority, and any change must land in both places in the same commit.

**Implementation status:** the endpoint itself is built in V0.3 (tasks B-301 to
B-305). The contract is fixed now so the web track can build against it.

## 9. WebSocket protocol (`/api/v1/screens/connect`)

Envelope for both directions:

```json
{"schema_version": 1, "type": "screen.command", "id": "msg_01J...", "sent_at": "2026-09-05T13:00:00Z", "payload": {}}
```

Server → client:

| type | payload | notes |
| --- | --- | --- |
| `session.ready` | `{screen: {id, name}, server_time, home_version, pending_command?: Command}` | first message after auth; `pending_command` is the newest `accepted` command not yet delivered, if still unexpired |
| `screen.command` | `{command_id, sequence, kind, payload, issued_at, expires_at}` | `kind = navigate \| show \| refresh` |
| `data.changed` | `{topics: ["home","photos","nas"], version}` | coalesced 250 ms per session; queue-overflow safe (dropped duplicates) |
| `heartbeat.ack` | `{server_time}` | |
| `session.revoked` | `{reason}` | followed by close `4002` |
| `session.superseded` | `{}` | followed by close `4003` |

Client → server:

| type | payload |
| --- | --- |
| `heartbeat` | `{route: RouteState, applied_sequence, client_version}` every 15 s |
| `state` | `{route: RouteState}` on route change |
| `command.ack` | `{command_id, status: applied \| failed, route: RouteState, resource_id?, error_code?}` |

`RouteState = {name: "dashboard" | "photos" | "photo" | "pair" | "connect", collection?: string, photo_id?: string}`.

Close codes: `1000` normal, `1001` server shutdown, `4001` unauthorized, `4002` revoked, `4003` superseded, `4004` send-queue overflow, `4005` protocol error. Messages larger than 64 KiB are rejected with `4005`.

## Related rules from the design

These live outside §9 but govern how the channel behaves; they are repeated here
so a client author does not have to cross-reference.

- **Authentication** (§6.8): the handshake carries the screen credential as the
  `atrium_screen` cookie or as `Authorization: Bearer`. Every handshake must
  also carry an `Origin` in `server.allowed_origins`; a handshake without one is
  refused. An unauthenticated handshake is closed with `4001`.
- **One session per screen** (§6.5): a new authenticated connection supersedes
  the previous one, which is closed with `4003`, so the same device with two
  tabs never executes a command twice.
- **Presence** (§6.5): a screen is `online` while it has an active session whose
  last heartbeat is within `screens.offline_after` (45 s).
- **Command application** (§6.5): the client applies a command only when
  `sequence > applied_sequence`, and otherwise acks `failed` with
  `error_code: superseded`. A duplicate `command_id` is ignored. The client never
  evaluates `expires_at` with its own clock; the server owns expiry.
- **Send queue** (§4.1): 64 messages per session. Overflow closes the session
  with `4004`.
- **Restart** (§4.2): every command still `accepted` at startup becomes
  `unknown` with `error_code: server_restart`.

## Server behaviour notes (V0.3 implementation)

These record where the shipped server is more specific than §9, or where §9
could not be met literally. The design remains the authority for the message
shapes; nothing below changes a payload.

- **Origin on the handshake.** A handshake that carries an `Origin` must have
  it in `server.allowed_origins`, exactly as §9 says. A handshake with **no**
  `Origin` is accepted when it presents `Authorization: Bearer`. A browser
  always sends an `Origin` and cannot set an `Authorization` header on a
  WebSocket, so this admits only non-browser clients (the CLI, a probe, a
  future MCP bridge) and admits no cross-site page. §6.8's general rule —
  "requests without `Origin` are only honoured with Bearer auth" — is what is
  implemented here.
- **Unauthorized handshakes.** Authentication happens before the upgrade, so a
  refused handshake is an HTTP `401` with the usual JSON error body and no
  socket is ever opened. Close code `4001` remains reserved for a session that
  loses its authorization after the upgrade.
- **Heartbeat timeout.** A session whose last heartbeat is older than
  `screens.offline_after` is reported offline immediately and then closed by
  the monitor with `1001`. §9 does not enumerate a code for this; `1001` is
  used because it is the "reconnect with backoff" signal the client already
  handles, and a closed session is exactly a client that should reconnect. A
  cleanly disconnected client is offline at once — the 45 s rule covers the
  session that stays open but stops speaking.
- **`pending_command`.** §9 describes it as "the newest `accepted` command not
  yet delivered". The server also redelivers a command that *was* delivered but
  never acknowledged, because that is the case a reconnect actually produces.
  Redelivery is safe under the rules already in §9: the client ignores a
  duplicate `command_id` and applies only a greater `sequence`.
- **`data.changed` under pressure.** It is the only droppable message. When a
  session's 64-message queue is full a `data.changed` is discarded rather than
  queued, because the client re-fetches on the next one; every other message
  type overflowing closes the session with `4004`.
- **Frame size.** The read path bounds each frame at 64 KiB + 1 byte and closes
  with `4005` itself, rather than letting the library close with `1009`.

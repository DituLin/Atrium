# Trusted archive ingress (T13)

This opt-in bridge captures native Weixin and Discord input before channel cursor acknowledgement or model dispatch. It does not use generic observer hooks as a media source. Source identity, original attachment positions, caption and declared metadata are frozen by the host. The model can select only `inherit`, `private` or `needs_clarification` for the current opaque input and can select current original text; it cannot supply identity, paths, bytes or replacement text.

## Runtime and installation

Ship the entire `adapters/openclaw` runtime (including all `archive-*.mjs` modules), existing `atrium-brain-host` and `home-mcp` binaries, and the new `atrium-spool-read` binary. Ship `scripts/patch-archive-ingress.mjs` together with its adjacent `archive-ingress-pins.json`. These are ordinary Node/Go runtime artifacts; no Codex process is needed. Node must support builtin SQLite (the existing adapter floor is Node 22.19; validation used Node 24.21).

Build the additional executable for the destination OS/architecture:

```sh
go build -o /destination/bin/atrium-spool-read ./cmd/atrium-spool-read
```

Add this `archives` object to the existing Atrium OpenClaw plugin configuration, preserving its existing agent, host and tools configuration. The example paths are deployment placeholders, not actual production changes:

```json
{
  "archives": {
    "root": "/private/atrium/brain/archive-inbox",
    "origin": "https://core.example:8443",
    "caFile": "/private/atrium/brain/core-ca.pem",
    "tokenFile": "/private/atrium/brain/ingest-token",
    "principal": "family-brain",
    "sourceReader": "/destination/bin/atrium-spool-read"
  }
}
```

Create `root` as a private directory with mode 0700 before startup. The spool is 0700; SQLite, staged originals and credential files are 0600. The token file must be a regular non-symlink file. The FD reader rejects symlinks at every source-path component, including ancestors; use physical canonical roots (for example `/private/var/...` rather than macOS `/var/...`). Keep credentials outside the plugin package. The integration token needs ingest permission; explicit private acceptance additionally requires Core read and organize permissions. A forbidden private acceptance remains restricted and never becomes inherited visibility.

Install the pinned native patches only against the package roots checked by the script:

```sh
node scripts/patch-archive-ingress.mjs --check /path/to/weixin-package /path/to/discord-package
node scripts/patch-archive-ingress.mjs --apply /path/to/weixin-package /path/to/discord-package
```

Pins target the inspected Weixin 2.4.9 and Discord 2026.9.8 installed sources, including existing local lossless-ID/SenderId compatibility changes. The script checks every original or already-patched SHA and exact anchors before applying; it rejects version/hash drift and supports repeat application. It patches source and runtime dist for Weixin and the actual Discord dist dispatch/preflight files. New backups use `.atrium-archive-original`; existing `.atrium-sender-original` backups are preserved. A package upgrade requires reviewed new pins. Patched native input requires the running bridge; absence fails closed before capture acknowledgement. Enable Core archive intake and plugin configuration as part of the same deployment. This implementation has only been patched into isolated copies, not the user's production packages.

The host's Core tool manifest is generated from its MCP inventory. `home_accept_archive_input` is a separate host-owned optional tool registered when archive configuration is present; it is not an invented MCP media path. Ensure the existing agent permits this optional tool. The new CLI completeness check uses the same named tool set as model projection; it preserves unknown-tool omission while rejecting missing required tools and duplicates. This fixes the subsequently discovered T12 startup regression where a literal count of 20 rejected the actual 21 Core tools.

## Persistence and recovery

Weixin capture runs native authorization and the monitor allowlist before committing the entire accepted batch. Only then can the original cursor advance. It persists every direct image/file/video/voice slot and preserves original positions even if one fails. Discord freezes the original raw payload at dispatch entry, disables coalescing while this bridge is active, and captures after native admission gates but before media/model processing and lifecycle adoption. Forwarded/referenced/history/sticker content does not become a direct archive slot. Voice originals remain original bytes; transcript text is not treated as the current original caption.

The private operational inbox contains bounded native recovery data for up to 24 hours. Ordinary successful chat processing clears raw input; downloaded slots clear provider credentials; expiry clears credentials including failed terminal slots. Raw recovery input is not sent to Core. Ordinary questions do not create text intakes. Completed identity tombstones and own receipts remain for replay detection. The active quota is 1024 messages and the unsaved spool quota is 1 GiB; capture fails before cursor advancement when capacity is insufficient.

Downloads use exact HTTPS provider hosts, reject redirects, enforce deadlines and streaming ciphertext/plaintext bounds, and report fixed error codes. Originals are limited to 32 MiB; AES-ECB ciphertext permits its fixed 16-byte padding overhead. Plaintext is published only after final decryption/integrity success. Files copied from a host allow-root use the no-follow FD reader, streaming hash/size validation, file fsync, atomic publication and directory fsync. Metadata and original slot identity do not change on retry.

Empty trusted captions durably select inherited visibility. Nonempty captions await the existing normal model turn, which receives the full original caption and current slot status. Intent is persisted before acceptance; `needs_clarification` stays pending without an automatic timeout default. OCR and quoted content cannot choose visibility. Retry uses persisted intent and never invokes a classifier or repeats the model turn.

The service starts a background reconciler and returns after bounded initialization; it is independent of the short-lived host process. It runs every five seconds, with per-slot exponential backoff capped at one hour, at most 32 attempts, and upload batches of eight. Exhausted attempts remain retained for operator investigation; they are not reported saved. Stop aborts active download/HTTP work and waits for pending staging/acceptance before closing SQLite. Each retry performs source lookup first and verifies saved hash/size before accepting the receipt. Missing sources upload the original private spool. A lost successful HTTP response therefore does not create a second source. Successfully saved spool bytes are removed after the durable own receipt is recorded.

Core origin, configured principal and credential digest bind the inbox. Credential rotation or a changed principal fails closed and needs a deliberate operational migration; do not point pending inputs at a different identity. Disabled/unavailable Core intake (including a route-level 404) remains retryable. A real source-not-found lookup response is distinct from an unavailable route. Ingest-only receipts contain only own intake/task ID, saved status, hash, size and acceptance time, without requiring document-read access. `staged` means temporary local storage, never saved in Atrium. Partial acceptance is reported per slot. Neither a Core intake receipt nor successful capture proves NAS backup or completed understanding/analysis.

## Verification and remaining rollout

Evidence is in `evidence/2026-10-11-t13`. Full Go, affected-package race tests, Node tests and Linux compilation cover the repository implementation. The actual TLS Core integration verifies cross-channel byte deduplication while retaining separate source receipts, private permission denial, and a built Brain CLI starting a real MCP child with the complete 21-tool inventory.

The Weixin harness executes the transformed installed runtime monitor with synthetic transport/admission/process dependencies. It proves capture-before-cursor and crash/restart replay for two slots. The Discord harness executes the transformed installed dispatcher, actual raw mapper and SDK queue/lifecycle with explicit preflight/model/ingress doubles; it proves capture before adoption and a consumed turn recovering two uploads without a second model call. These are actual native module tests, not real network delivery.

The retained `*-normal-model.json` files come from two actual isolated OpenClaw/DeepSeek normal tool-calling turns, a real TLS Core and real Brain/MCP binaries. A test-only wrapper supplies a synthetic trusted current input context. The private/no-TV caption selected private; the opposite “不要设为私密，允许在 TV 展示” caption selected inherit. These runs validate semantic intent/tool/receipt integration, not the live Weixin/Discord transport. Captures are retained unchanged; later rendering cleanup does not rewrite historical captures.

Production enablement, deployment of these artifacts, and new user-sent Weixin/Discord files remain T15/T17 rollout checks. No production configuration, family database, channel package or service was modified by these tests, and no external IM was sent. TV presentation and NAS completion remain their separate tasks.

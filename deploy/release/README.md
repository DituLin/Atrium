# Local archive release

[构建、发布与部署（中文）](../../docs/ops/deployment-release.md) · [Build, release and deployment (English)](../../docs/ops/deployment-release.en.md)

The paired guide distinguishes the existing local tools below from hosted candidate builds and independent automated promotion that are not yet connected.

Build from a committed SHA on Darwin arm64 using `make release-package
RELEASE_DIR=/absolute/new/package RELEASE_SHA=<sha>`. Explicitly set
`ATRIUM_RELEASE_NODE`, `ATRIUM_RELEASE_OPENCLAW`, and
`ATRIUM_RELEASE_PROVIDER` to the existing trusted runtime bindings. The builder
checks Node 24.21.0, OpenClaw/provider 2026.9.8 and the provider's OpenClaw link;
it does not install dependencies or change channel packages. The archive worker
also checks its full bounded runtime fingerprint. Build output contains the
fresh web bundle embedded in Core, all four Go commands, complete plugin and
worker resources, channel patch/pins, native helper, three exact codecs and
licenses. A failed build leaves its owned destination for inspection; choose a
new destination for a retry.

Use the existing signing identity on `bin/*` and `native/helper` only. Never
recursively sign the codec libraries (their full SHA is pinned in the helper).
After signing run `python3 scripts/manifest.py <package>`, then the same command
with `--verify`. Perform relocation/native-format smoke tests with the final
signed package and a sanitized environment. The manifest hashes the signed
bytes. Keep packages immutable after verification.

Core accepts `serve --provider-env-file /protected/service.env`. This path alone
may be placed in launchd arguments. The file must be owned by the service user,
0600, regular, single-link and contain precisely one unquoted
`DEEPSEEK_API_KEY=<value>` line. It is not a shell script. Key values must never
appear in plist, command arguments, logs or release manifests. No option keeps
the existing process environment behavior.

Stop gateway and Core for the final pre-switch backup. Capture a consistent
SQLite backup, archive originals, config, TLS/credentials, and Brain
inbox/ledger/spool before atomic executable/config switching. Do not simply
copy a live SQLite main file without its WAL. Explicitly exclude rebuildable
photo cache; preserve originals and pending IM work. Preserve NAS mount,
automatic login and signing configuration.

Start with archives disabled, run `atrium maintenance schema --config ...`,
then `atrium maintenance backfill --config ...`. Backfill works with the feature
flag off and can be interrupted/repeated. It scans every historical record,
rereads the current chain inside each projection transaction, drains the
incremental outbox and catches up the search index. Its JSON output contains
counts only. A successful count is not proof of model parsing: independently
compare all original rows/history and stable legacy event IDs before enabling
archives. Re-running backfill handles old corrections without high-watermark
skips. Existing IM principal/tool configuration and pending inboxes must survive
the upgrade. Generate the model tool inventory from the exact packaged Brain
and MCP binaries before restarting gateway.

Before rollback, stop writers and take a **new** consistent snapshot of current
post-upgrade DB, archive originals and Brain state. Never restore the old
pre-upgrade DB over the live data: this would lose new records. Before invoking
an old executable, run `schema-guard.py <db> <verified-target-migrations-dir>
<target-max-schema>` (the pre-upgrade executable supports schema 8). The checker
uses read-only SQLite and requires an exact migration-name/version prefix. A
rejection means keep the new executable/data or restore into a separate local
directory and reconcile; it never downgrades or launches a binary itself.
Keep writers stopped between this check and any manual old-binary invocation.
New binaries also reject unknown/mismatched/holey histories on every store.Open
and Migrate path.

This package is a local archive launch. Full immutable NAS archive snapshots
and restore remain separate T16 work: NAS status must remain pending until a
verified snapshot exists. Building/deploying does not claim external IM delivery,
a one-hour sustained test, final T18 audit, or full M1–M5 acceptance.

# TV dark UI deployment verification

[中文](README.md)

Deployed on 2026-10-20 with explicit user authorization.

- Frontend commit: `041b4833678109735c5799de6d1f65ac88b11067`.
- Backend source, dependencies and migrations remain at deployed commit `c06c6a591e9a33c6a93bc0def15d3e29061f9d07`. Core was rebuilt with the new production Web bundle. This is not an upgrade of the complete main branch, model runtime or channels.
- Running version: `tv-ui-041b483+c06c6a591e9a33c6a93bc0def15d3e29061f9d07`.
- The existing complete package passed verification. The candidate replaces only Core and adds provenance; the other three programs, native helper, codecs, plugins and runtime bindings retain identical bytes. The package manifest retains the backend baseline commit, while `build-provenance.json` records the frontend commit, overlay scope and bundle hashes separately.
- Core uses the existing `io.atrium.core` identity with the same designated requirement. The stable maintenance CLI, configuration, worker/helper paths and provider bindings were not modified.

Before activation, the actual candidate Core ran against isolated data. Nine synthetic feeding/height/weight records supported 56 API requests: all six pages, growth charts, weekly milk totals, settings refresh and WebSocket presence passed without page errors. Frontend validation passed 486 tests across 69 files, lint, type checking, production build and the Chrome 66 CSS build guard.

Core and the OpenClaw gateway stopped before the final backup, and database writer handles were confirmed released. The private consistent backup preserves databases, archive originals, configuration, TLS/credentials, Brain intake/ledger/spool, channel state and old executables; rebuildable photo cache and unchanged external runtime directories are excluded. SQLite backup, integrity/FK checks and family business-table hashes passed. After atomic activation, live/ready report the target version, served CSS/JS hashes match the candidate, schema 17 and existing family records remain intact, and Core/gateway are running.

The official `io.atrium.tv` app on OnePlus6T was refreshed. Its WebView 119 loaded the new CSS/JS. Dark surfaces and viewport bounds on all six pages, native ADB remote menu and domain navigation, and client version checks passed without page errors. The app returned to Home afterward. This device acceptance covers OnePlus only; physical TCL / Chrome 66 visual, performance and video decoding checks were not repeated.

The predeployment backup already marked the NAS photo source offline; it remains offline afterward, with the last successful read on 2026-10-19. This pre-existing condition is neither claimed as a deployment regression nor marked fixed.

See [verification.json](verification.json) for sanitized results. Full manifests, backups, API records and screenshots containing household material remain in protected local storage. Rollback keeps the current database and switches only to the previous Core with the same schema; old backups must not overwrite newly received material.

# Atrium

[中文（主文档）](README.md) | English

Atrium is a local-first home information hub evolving into a family AgentOS: a Mac mini runs Core, a NAS supplies read-only photos and videos, and a TV displays household information. Core manages archive material, permissions, evidence and task state.

The current code includes Go Core, a React TV client, `home-mcp`, a bounded Brain host and OpenClaw/DeepSeek integration. AI is optional; basic LAN display and media services do not depend on a model or cloud account. When understanding or conversation is enabled, the configured external model service may receive prompts, the current request, selected extracted text and evidence context, or tool results authorized for that request. Native image/PDF parsing runs in the local helper.

## Current delivery status

The project remains in pre-release and ongoing acceptance. The integrated archive has local implementations and deployment verification on the host; the [implementation and acceptance record](docs/ops/atriumos-implementation-status.md) defines the complete delivery boundary.

| Acceptance area | Current boundary |
| --- | --- |
| Archive intake, parsing, classification, attribution, retrieval, analysis and TV display | Implemented, with task-level tests and host verification |
| Real channel integration | New WeChat text verified; real attachments and new Discord message acceptance pending |
| T15 system acceptance | Partially passed; the complete model and system cases have not all been accepted |
| T16 complete NAS backup and recovery | Complete immutable archive snapshots and the recovery closure remain to be implemented |
| T17 deployment and sustained acceptance | A fixed version is deployed on the host; the one-hour sustained run and 10,000-document performance acceptance remain pending |
| T18 final audit | Remaining acceptance, recovery and final delivery audit are pending |

Host and OnePlus development-device verification does not replace real-TV, reboot, 24-hour or seven-day stability acceptance, or establish that every general family AgentOS capability is complete.

## Household display and control

| Feature | Current capability |
| --- | --- |
| Home dashboard | Household-zone time and date, media display, Core/NAS/connection state |
| Photos | Read-only indexing, preview and thumbnail cache, collections, single-photo viewing and slideshow |
| Videos | Video browsing and playback from read-only media sources; availability depends on format and host processing support |
| Daily briefing | Household information, time and status from configured sources |
| House | Configured house information and source status |
| Calendar | Source interface and synchronization status are retained; it is unconfigured by default, not a completed calendar service |
| Archives | Person/domain/type filters, evidence and original viewing, review state and bounded analysis |
| Screen control | CLI/API `navigate`, `show` and `refresh`, with actual execution receipts |

The TV can use a browser or the standalone Android host. Remote-control browsing and public projections share the same client; completed background tasks do not automatically take over the current page.

## Integrated archives

- Automatic classification prioritizes health, education, finance and home documents, retaining domains, document types and extensible labels.
- Local parsing supports text, Markdown, CSV, DOCX and XLSX; the macOS helper provides extraction and previews for supported image formats and PDFs.
- Originals, extracted facts and model proposals are stored separately. Provenance leads back to text spans, pages or original components; one document can link multiple people and roles.
- Attribution uses subject roles and original evidence. Duplicate names, missing identity or conflicts remain under review; a sender or model confidence cannot directly establish subject identity.
- Retrieval and natural-language tools follow current authorization and public projections. Core computes basic counts, sums and changes from archived data, disclosing coverage and missing material.
- Encrypted, unsupported, corrupt or over-limit content has an explicit processing state. Retaining an original does not mean all content was extracted; XLSX formulas and macros are not executed.

These capabilities support document organization and verifiable analysis. Autonomous household actions, complete NAS recovery and acceptance for all real channels still have separate delivery gates.

## Architecture and storage

```text
TV browser / Android host ← HTTPS + WebSocket → Atrium Core → read-only NAS media
                                                  │
CLI / home-mcp / Brain / OpenClaw → scoped APIs    │ local SQLite + originals + cache
                                                  └ optional external model service
```

Core owns durable material, household-scoped authorization, background tasks and evidence. MCP exposes controlled tools; the Brain host and OpenClaw provide conversation integration and bounded orchestration, without bypassing Core to write its database.

SQLite, indexes, original buffers and caches reside on the local disk; SQLite is not placed on SMB. NAS photo/video directories stay read-only, while legacy family-record JSON export uses a separate writable directory. Host operational backups and legacy exports do not replace T16 complete archive backup and recovery.

## Development quick start

Requires Go 1.25+ and Node 24+; the Node development mock uses native TypeScript type stripping.

```sh
make web
make build
./bin/atrium init --config ./config.yaml --data-dir ./data --public-url https://127.0.0.1:8443
# Edit config.yaml: home.timezone and sources[0].root (read-only NAS mount)
./bin/atrium serve --config ./config.yaml
```

For TV access, replace the example `--public-url` with the Mac mini's actual LAN HTTPS address before `init`, so the certificate matches its hostname/IP. For host-only development, use `https://127.0.0.1:8443`.

Before the first HTTPS visit, export the public CA with `./bin/atrium tls export-ca --config ./config.yaml` and configure browser trust following the [operations runbook](docs/ops/runbook.md). For the Android host, provision `ca.crt` as described in its [build guide](android-tv/README.md). Once trust is configured, the TV shows a six-digit pairing code at the configured address. Approve pairing and control the display from the Mac mini:

```sh
./bin/atrium admin pair approve 123456 --id living_room_tv --name "Living room TV" --config ./config.yaml
./bin/atrium admin screens list --config ./config.yaml
./bin/atrium admin screen navigate living_room_tv --route photos --collection captured_today --wait --config ./config.yaml
```

The admin token is stored in `data/admin.token` (0600). `./bin/atrium tls export-ca --config ./config.yaml` exports the public CA certificate; private keys and the admin token stay in protected host locations. Read [deployment and release](docs/ops/deployment-release.en.md) and the [operations runbook](docs/ops/runbook.md) before deploying.

## Development checks and builds

`make check` checks the Go track: `go vet`, golangci-lint, Go tests and builds of the three Go programs. It requires an installed golangci-lint and does not execute Web checks.

```sh
make check
cd web
npm ci
npm run lint
npm test
npm run build
cd ..
make web-sync
```

`make web` installs and builds the frontend and synchronizes Go embed assets; `make build` builds `atrium`, `home-mcp` and `atrium-brain-host`. CI runs Go, Web and Shell checks separately and generates macOS arm64/amd64 binary build artifacts.

## Android TV host

[`android-tv/`](android-tv/README.md) provides a standalone TV/mobile container using Android System WebView, with direction and Back keys. Chrome and a Google account are not required.

The current Android delivery is a development-test APK with debug signing and WebView debugging enabled; it is not a public production release from CI. Household CA input is only the public `ca.crt` certificate. Never bundle `ca.key`, `server.key` or the admin token. Follow the Android documentation separately for production signing and device acceptance.

## Privacy and data boundaries

- Paired screens, administrators and integration clients use separate credentials and permissions. The TV reads only authorized public projections.
- Photo-source symlinks are not followed; NAS credentials and source paths are not exposed to the TV. Photo previews are re-encoded to remove EXIF/GPS.
- Local configuration, data, tokens, certificate private keys and real household material belong in protected, ignored locations. Review new documentation and build artifacts before committing.
- Documentation examples use placeholders or synthetic material. Historical documents may retain old machine paths and earlier status; check them against current code and acceptance records.

## Documentation and contributions

- [Documentation index](docs/README.md)
- [部署与发布](docs/ops/deployment-release.md) · [English deployment guide](docs/ops/deployment-release.en.md)
- [Implementation and acceptance record](docs/ops/atriumos-implementation-status.md)
- [AtriumOS system design](docs/architecture/atriumos-system-design.md)
- [Integrated archive design](docs/plans/2026-10-03-family-agentos-archives-design.md)
- [Android TV build and usage](android-tv/README.md)

Chinese is the primary documentation language; English is maintained as a complete parallel version. New or substantially rewritten user-facing documentation should have matching Chinese and English versions. Historical documents retain their original language until rewritten. Distinguish design goals, current code and actual acceptance status.

## License

[MIT](LICENSE).

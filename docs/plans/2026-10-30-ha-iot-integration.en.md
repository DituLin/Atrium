# Home Assistant IoT Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver a read-only loop from real devices through Home Assistant on the NAS and Atrium on the Mac to the TV House screen, while preserving boundaries for future authorized control.

**Architecture:** HA owns device protocols and vendor accounts. Atrium maps HA entities into brand-independent capabilities. Background synchronization persists observations; HouseComposer reads public projections only. The TV connects exclusively to Atrium.

**Tech Stack:** Go 1.25, existing coder/websocket, SQLite, React 18 / TypeScript, Home Assistant Container, Synology Docker / Container Manager.

[中文](2026-10-30-ha-iot-integration.md)

---

## 1. Status, authorization and decisions

Drafted on 2026-10-30; initial scope approved and implementation underway on 2026-11-04. The user authorized the NAS upgrade, HA deployment and import of the selected Xiaomi household. The normalized model and HA subscription adapter are implemented, and real HTTPS/WSS synchronization is verified. Storage, House-screen integration and end-to-end acceptance remain in progress.

### Verified progress on 2026-11-03

- NAS upgraded to DSM 7.4.1-90080 Update 1, Container Manager 24.0.2-1706 and Docker Engine 24.0.2. The environment table below retains the historical pre-upgrade snapshot.
- Home Assistant Container 2026.10.4 deployed with a pinned image digest, NAS-local configuration, host networking and automatic restart. Official Xiaomi integration v0.5.0 installed and account authorization completed.
- The selected household imported 15 devices and 308 entities. Humidifier, thermometer/hygrometer and aquarium pages displayed valid readings. One inspected smart plug currently has unavailable entities. Import counts do not establish availability of every device or capability.
- The actual scale is an S400, rather than the initially reported Scale 2; the inventory also includes a XiaoAI mini speaker. Subsequent mappings must use observed models and entities.
- No device controls were executed. Continuous updates, device-to-HA latency, HA-to-TV latency and fault recovery remain unverified.
- Initial setup used LAN HTTP on that date. Dedicated credentials, an entity allowlist and verified HTTPS/WSS were completed on 2026-11-04; the candidate adapter synchronized 13 capabilities.
- Both Atrium NAS photo sources returned online. Automatic remount after NAS restart remains unverified. Backup scope follows the user request: configuration only, with no further photo or storage-content backup.

### Confirmed

- The NAS is the preferred HA host; Atrium stays on the always-on Mac mini.
- Phase one is read-only. Future control is granted separately per screen and may be restricted to specific devices; screens default to read-only.
- TVs never connect directly to HA. Mac–NAS communication uses LAN APIs, not SMB or a shared database.
- Non-Xiaomi, ESPHome and MQTT devices can enter HA first and reuse the Atrium HA adapter.
- MiGPT Next was stopped at the user's request; its container and data remain. This plan neither restores nor deletes it.

### Inspected environment (historical snapshot, not a capacity guarantee)

| Component | Observed on 2026-10-30 |
| --- | --- |
| Mac | M4, 10 cores, 32 GB RAM, macOS 15.2; about 6 GiB internal disk space free |
| Atrium | Running as a per-user LaunchAgent with launch-at-login and keep-alive |
| Mac power | System sleep disabled; automatic startup after power failure disabled |
| NAS | Actual model **DS220+**, not the previously reported DS226+ |
| NAS system | DSM 7.1-42661 Update 3; Intel Celeron J4025, two cores, 10 GB RAM |
| Containers | Docker package 20.10.3-1308 running; approximately 2.11 GB RAM used at inspection |
| NAS peripherals | DSM detects a USB UPS; shutdown and recovery policy not validated |

Public documentation must not contain real accounts, addresses, serial numbers, tokens or unredacted screenshots.

### Approved initial design

- Acceptance devices: humidifier, thermometer/hygrometer and aquarium. The unavailable plug is deferred until it returns online.
- Group devices by room with an “Unassigned room” fallback; configure rooms, ordering and visible capabilities in a file initially.
- One HA instance and latest state only; no history charts, automation editor or device-management UI.
- Add an independent `iot` field while retaining `profile` and `environment`; avoid restructuring the overview at the same time.
- Under healthy network conditions, target HA-state-receipt to TV-render latency of at most 5 seconds. Measure vendor/device-to-HA delay separately; this is not a real-time SLA.

## 2. Deployment prerequisite: compatibility before installation

Current HA Container documentation requires Docker Engine 23.0.0+. The old NAS package cannot be assumed compliant. Verify the actual Engine, architecture, supported DSM upgrade path and Engine bundled with the target Container Manager. **Do not assume that any DSM 7.2 upgrade is sufficient.**

Prepare an impact inventory: existing containers, volumes, ports, package compatibility, recoverable backups and a maintenance window. Do not assume DSM supports in-place downgrade. Present exact target versions and impacts for the user's upgrade decision. Installation or upgrades happen only after overall plan confirmation.

If a suitable runtime cannot be obtained, pause production installation and continue local mock-HA development. Separately assess HAOS in a NAS VM or a Mac VM after storage is freed. Do not silently use obsolete HA to bypass requirements.

Deployment rules:

- Put HA configuration on a NAS-local volume mounted at `/config`, not on the Mac's SMB mount.
- Pin tested HA image version/digest and Xiaomi integration version. Do not track `latest` in production.
- Configure container restart and log rotation. Propose seven days of HA history with unnecessary entities excluded; verify syntax against the chosen version.
- Reserve a NAS address. Provide Atrium a stable HTTPS/WSS endpoint with verifiable certificates, potentially through a LAN reverse proxy. Do not disable certificate validation.
- Initial cloud integration does not require USB passthrough, host Bluetooth or blanket privileged mode. Validate network mode and minimum permissions against the selected HA/integration versions.
- Back up HA configuration and Atrium state. HA configuration includes secrets; store backups only in controlled locations.
- Verify Mac and NAS startup independently. Current Mac launch-at-login behavior does not establish unattended recovery before login.

## 3. Device matrix

| Device | Route | Initial boundary |
| --- | --- | --- |
| Mijia Smart Plug 3 | Official `xiaomi_home` | Switch state; reported power/energy issues exclude energy from acceptance |
| Mijia Smart Antibacterial Humidifier 2 | Official integration candidate | Operating state; humidity/modes depend on actual entities |
| Xiaomi Smart Fish Tank | Official integration candidate | Inventory first; temperature/feeding support unconfirmed; no power or feeding actions |
| Mijia/Xiaomi Air Circulation Fan | Official integration candidate | Verify exact model and power/speed/oscillation mappings after import |
| XiaoAI Speaker Play / Pro | Official integration candidate | No promise of announcements, playback or Atrium voice input; outside initial acceptance |
| Mijia Bluetooth Temperature and Humidity Monitor 2 | Xiaomi BLE, verify label model | Resolve Bluetooth coverage and bindkey separately; neither NAS nor XiaoAI is assumed to be an HA Bluetooth proxy |
| Mijia Body Composition Scale 2 | Xiaomi BLE / ESPHome, model-dependent | Weight/impedance; identity and health privacy require separate design; exclude from public House screen |
| Custom ESP devices | ESPHome native API | Reuse supported HA entity capabilities; no toolchain deployment in phase one |
| Raspberry Pi custom programs | MQTT Discovery or REST | Add a broker only when needed, not in advance |

Product names do not replace model and hardware verification. Record only a sanitized capability inventory: entity domain, type, unit, availability and update mechanism. Users authorize Xiaomi accounts inside HA; passwords do not pass through chat.

## 4. Unified model, synchronization and failures

### Model

`IntegrationInstance → Device → Capability → Observation`; Atrium configuration owns room assignment.

- Instance: stable ID, provider, connection status, synchronization generation and last successful synchronization.
- Device: stable Atrium ID, display name, room_id and instance. Mutable names are not identities.
- Capability: stable ID, kind, type, canonical unit and provider entity binding. Initially `power` (boolean), `humidity` (number/%), `temperature` (number/°C) and `operating_state` (allowlisted enum), plus `battery` and `filter_life` (number/%) for the confirmed device scope.
- Observation: typed value or null, source state timestamp `source_updated_at`, `sampled_at` only when explicitly provided, `received_at`, `confirmed_at`, availability and public reason.
- Entity renames preserve Atrium IDs through explicit rebinding. Missing bindings remain missing rather than being matched by name. Multi-platform duplicate devices are mapped manually initially.

Track connection and entity availability independently. Never convert `unknown` to zero/false or interpret `unavailable` as switched off. Reject invalid values and units instead of guessing; temperature conversion accepts known units only.

### Proposed configuration (synthetic example only)

```yaml
iot:
  enabled: false
  home_assistant:
    id: ha_primary
    url: https://ha.example.invalid
    token_file: /path/to/private/ha.token
    devices:
      - id: demo_plug
        name: Demo plug
        room_id: demo_living_room
        room_name: Demo living room
        capabilities:
          - id: power
            kind: power
            entity_id: switch.demo_plug
```

Reject duplicate IDs, entity bindings duplicated across devices, unknown capabilities, URL user information and inline tokens. Restrict token-file access to the service account and redact logs/configuration output. Use a dedicated non-admin HA account and verify its actual readable scope. An ordinary long-lived token is **not inherently read-only**. The initial Atrium client implements only allowlisted read messages, with no generic proxy or action calls.

Configuration changes require restart initially. Filter against current allowlists at startup and on every projection, removing revoked observations. Disabling integration or losing authorization must not keep exposing previously cached sensitive data.

### Synchronization algorithm

1. Authenticate a background worker. Subscribe and buffer events before requesting full state, avoiding the snapshot-before-subscription gap.
2. Isolate connections by generation. Merge full state and buffered updates by reliable provider version/time; never overwrite a newer event with an older snapshot. Resynchronize if ordering cannot be established.
3. Atomically publish the first complete synchronization. Persist subsequent meaningful changes and emit an `iot` notification.
4. Proposed heartbeat interval: 20 seconds, timeout: 10 seconds; jittered exponential reconnect backoff: 1–60 seconds. Authentication rejection enters a reauthorization-required state without rapid retries.
5. Propose full reconciliation every five minutes. These are configurable design defaults, to be verified against the selected HA version.
6. On disconnect, retain explicitly old observations with a failed connection marker. Restore availability only after full resynchronization. Persisted observations start stale after Atrium restart as well.
7. HA entity removal clears the current value and marks it missing. Removing Atrium display authorization removes the entire displayed item.

Connection health, entity availability and fresh sampling are different facts. Heartbeats must not advance sample time. An unchanged switch does not expire merely because `last_changed` is old. Without a genuine sample timestamp, display “last synchronized,” not “just measured.” Sensor expiry requires reliable sample evidence and a capability-specific threshold.

Persist only the latest allowlisted observations, not complete HA payloads or long-term history. Use one table keyed by instance/device/capability; notify TVs only after successful commits. NAS/HA outages must not block Core startup, photos or archive APIs.

## 5. API and TV

- Add optional `iot` to `/api/v1/family/house`. Preserve existing fields. Verify schema-version-1 additive compatibility with old clients; revise versioning before implementation if incompatible.
- Include integration availability, room/device lists and per-capability states. Exclude tokens, HA URLs, external entity IDs and raw errors.
- Leave `profile` and `environment` as existing placeholder contracts initially, avoiding changes to `OverviewSources`. New House UI uses IoT readings when present and avoids a contradictory “environment not connected” message.
- New clients must still work against old servers without `iot`.
- Add `data.changed` topic `iot`, refreshing House only, without unnecessary photo-statistics reads. Preserve coalescing, request generations and authorization clearing.
- Routine updates must not blank the entire screen; revocation must clear immediately. Keep the existing 30-second visible-page polling fallback and stop polling in the background.
- Proposed room-grouped device cards include an unassigned fallback; Core/NAS remain separate. No control buttons, unusable actions or invented healthy values.
- On TV–Atrium disconnect, retain an explicitly stale public snapshot. Clear on pairing revocation/401. Preserve remote focus and back-navigation behavior.

## 6. Future control boundaries (not implemented now)

Introduce a separate Controller later, without exposing HTTP write endpoints in phase one. Flow: TV → Atrium authorization/parameter checks → HA action → device-state confirmation.

- Screens default to no control permission. Restrict both devices and actions; never forward arbitrary HA services.
- Persist operation IDs with parameters for deduplication. `accepted` is not `confirmed`; timeout means unknown outcome followed by state reconciliation.
- Do not promise cross-device exactly-once execution. Prefer set-on/set-off over toggle; never automatically retry non-idempotent feeding or cleaning-start actions.
- Do not queue offline commands for later execution. Reconnection only resynchronizes state; uncertain outcomes are not successes.
- ESPHome/MQTT devices reuse supported capabilities; new capabilities and special actions require explicit contract extensions.

## 7. Staged implementation tasks

Write critical behavior tests, observe failure, implement, then verify passing results. Produce separate commits per stage; keep production installation separate from code commits. New paths below are planned; recheck migration numbering when implementing.

### T0 — Runtime compatibility and deployment review (production gate)

Create `docs/ops/ha-iot-deployment.md` and `.en.md` with target versions, compatibility evidence, package impacts, backups, maintenance window and recovery steps. Use synthetic paths only.

1. Read-only inventory of DSM, Engine, containers/volumes/ports and available resources; never print environment secrets.
2. Check official upgrade paths and target Engine; confirm Photos/SMB/container compatibility and data backups.
3. Specify Mac–NAS addressing, TLS trust, network access and startup; identify user-operated account authorization steps.
4. Upgrade/install only after the user approves the concrete deployment proposal. Return to architecture selection if infeasible.

Pass: exact targets and recovery are established; merely opening Docker is insufficient.

### T1 — HA device inventory (separable from local development)

Planned files: `deploy/iot/compose.example.yaml`, `docs/ops/ha-iot-deployment{,.en}.md`.

1. Run pinned HA on a T0-compliant host and validate restart/backup restoration. Do not restart MiGPT Next.
2. Install pinned official Xiaomi integration; the user authorizes the correct region/home.
3. Inventory real plug/humidifier entities, switch semantics, humidity units and unavailable states. Exclude power consumption from acceptance.
4. Change state through Mi Home and observe HA; record HA-side delay and sanitized capabilities.

Pass: required states from two device categories update in real HA; mocks cannot substitute for hardware evidence.

### Pre-development connection checklist, 2026-11-04

Code inspection confirms that `internal/config/config.go` has no IoT configuration, the environment projection in `internal/family/house.go` remains `NotSupported`, and `web/src/screens/HouseScreen.tsx` shows only the environment placeholder. The highest database migration is currently 0017.

Initial scope confirmed by the user on 2026-11-04: humidifier, thermometer/hygrometer and aquarium. The unavailable plug no longer gates initial acceptance.

| Capability | Initial display | Source |
| --- | --- | --- |
| Thermometer/hygrometer | Temperature, humidity, battery | Individual sensor entities |
| Humidifier | Power/operating state, current and target humidity, ambient temperature | Humidifier state and attributes, separate sensors |
| Aquarium | Water temperature, pump status, remaining filter life | Individual sensor entities; no feeding or reset controls |

Read actual entity IDs, units and valid states from HA entity details before implementation; do not infer IDs from device names. Real mappings belong only in private runtime configuration; repository examples use synthetic IDs. Distinguish `last_updated` from measurement time and never label resynchronization time as a sensor sampling time.

Continue with the independent HA WebSocket adapter: full synchronization plus event subscription, normalized capabilities and the House API projection. REST polling may aid diagnostics but should not become a second production synchronization mechanism. Embedding HA in the TV would conflict with the agreed permission and presentation boundaries.

Production prerequisites remain: verified HTTPS/WSS, a dedicated non-administrator HA account with a local credential file, and a verified entity allowlist. HA accounts/tokens are not inherently read-only; the Atrium client must restrict outgoing messages to reads and never invoke control services. Do not extract tokens from browser sessions or ask users to paste tokens into chat. Hand off credentials through private local files; never expose credentials or raw HA addresses through the TV API.

Follow T2–T6 for application code and enable production configuration only after real entities and connection prerequisites are ready. T7 still requires real updates from at least two device types, reconnection and TV latency checks; screenshots of current readings are not a substitute.

### T2 — Unified model and configuration

Create `internal/iot/types.go`, `normalize.go`, `normalize_test.go`.
Modify `internal/config/config.go`, `load.go`, `validate.go`, their tests and `config.example.yaml`.

1. Test unknown/unavailable, valid zero, unit conversions, duplicate mappings and secret redaction.
2. Implement the model and disabled default; old configurations still start.
3. Introduce read-only `SnapshotReader.Read(ctx)` and adapter `Run(ctx, sink)` interfaces. Final signatures are established during implementation; do not add control or a full SDK.
4. Run `go test ./internal/config ./internal/iot`; expect passing tests.

### T3 — HA subscription adapter

Create `internal/iot/homeassistant/client.go`, `sync.go`, `client_test.go`, `sync_test.go`.

1. Build a synthetic HA server using Go httptest and existing coder/websocket.
2. Test auth rejection, snapshot/event interleaving, late old connections, removal, reconnection, cancellation and jittered backoff.
3. Implement read-message allowlists, message/queue limits, timeouts and reconciliation. Queue overflow triggers resynchronization.
4. Run `go test -race ./internal/iot/...`; expect passing tests. Assert no `call_service` is sent and logs contain no tokens.

### T4 — Persistence and worker lifecycle

Create `internal/store/iot.go`, `iot_test.go`, `internal/app/iot.go`, `iot_test.go`.
Proposed migration: `internal/store/migrations/0018_iot_observations.sql` (currently maximum 0017; allocate again at implementation).
Modify store migration registration/version gates, `internal/app/runtime.go`, `lifecycle.go`, `internal/domain/enums.go`, `internal/app/events/coalesce.go` and their tests.

1. Test atomic snapshot replacement, withdrawal, stale startup, current allowlist filtering and old-generation rejection.
2. Implement latest-state persistence and worker lifecycle; network failures do not block the main service.
3. Add `iot` topic, publishing after persistence and suppressing redundant high-frequency notifications.
4. Run `go test -race ./internal/store ./internal/app/... ./internal/iot/...`; expect passing tests.

### T5 — House projection and contracts

Modify `internal/family/types.go`, `house.go`, `house_test.go`, `internal/httpapi/server.go`, `family_test.go`, `docs/api/openapi.yaml`, `docs/api/ws-protocol.md`, `internal/briefing/composer_test.go`.

1. Test unconfigured, initial sync, partial unavailability, HA outage, authorization withdrawal and NAS failure isolation.
2. Inject SnapshotReader and add public IoT DTOs. The existing early return on NAS read failure must not skip independent IoT projection.
3. Verify API schema, old fields and overview compatibility. Provide corresponding bilingual interface documentation under repository conventions.
4. Run `go test ./internal/family ./internal/httpapi ./internal/briefing ./internal/ws`; expect passing tests.

### T6 — TV rendering and refresh

Modify `web/src/types/api.ts`, `types/ws.ts`, `app/house.ts`, `app/dataChanged.ts`, `app/useConnection.ts`, `screens/HouseScreen.tsx`, `styles/home.css`, `test/houseFixture.ts` and related mocks.
Tests: `web/src/app/house.test.ts`, `dataChanged.test.ts`, `houseLifecycle.test.tsx`, `screens/HouseScreen.test.tsx`.

1. Synthetic fixtures cover switch-off, zero humidity, unknown, stale, partial failures and removal.
2. Implement room cards and source status, support responses without `iot`, and add no controls.
3. Handle IoT notifications while retaining authorization generations, late-response protection, background suspension and stable focus.
4. Run `npm --prefix web test -- src/app/house.test.ts src/app/dataChanged.test.ts src/app/houseLifecycle.test.tsx src/screens/HouseScreen.test.tsx`.
5. Run `npm --prefix web run typecheck`, `npm --prefix web run lint`, `npm --prefix web run build`; all must pass.

### T7 — Integration verification and real TV acceptance

Create synthetic helpers under `scripts/e2e/iot/` and sanitized evidence in `docs/ops/evidence/<execution-date>-ha-iot/README.md` and `.en.md`.

1. Run `go test ./...`, `go vet ./...`, `npm --prefix web test`, and repository release checks through `make check`. Resolve missing lint tooling; do not claim complete checks when unavailable.
2. Browser-test 1920×1080, 3840×2160 and 804×384, long names, multiple rooms, scrolling and remote navigation.
3. Deploy a candidate through existing release procedures; test humidifier, thermometer/hygrometer and aquarium state changes, outages, recovery, device withdrawal and authorization revocation on the real TV; the plug is not an initial gate.
4. Measure device→HA and HA→TV separately. Target HA→TV ≤5 seconds; repeat at least ten times and retain failures.
5. Verify 30-second polling fallback plus request time after missed notifications, full resynchronization, and photo/archive independence when initial HA connection fails.
6. Under the agreed maintenance arrangement, test HA, Atrium and host restarts. Distinguish post-login from unattended startup.
7. Only after real acceptance update `README.md`, `README.en.md` and implementation status. Commit sanitized evidence only.

## 8. Rollback and completion

- IoT defaults disabled. Disable and restart Atrium to restore the original House behavior without deleting device/NAS data.
- Do not run an old binary against an unsupported migrated schema. Follow existing release schema guards and restore matched backups when necessary without overwriting intervening family data.
- Roll HA back using a matching image and pre-upgrade configuration backup, not an older image against incompatible data.
- Review DSM recovery separately; container rollback does not restore DSM.
- Completion requires compatible runtime, two real device categories, TV rendering, recovery, authorization clearing, tests and sanitized evidence. Mock success establishes software-stage completion only.

## 9. Review checklist

- [x] NAS upgrade and HA deployment authorized and completed (2026-11-03).
- [x] Initial devices changed to humidifier, thermometer/hygrometer and aquarium (2026-11-04).
- [ ] Room grouping with an unassigned fallback and file-managed mappings approved.
- [ ] Independent IoT module, latest-state storage and read-only scope confirmed.
- [ ] Network/TLS, startup recovery, maintenance window and acceptance targets confirmed.
- [x] User authorized development for the three device types (2026-11-04); production credentials and hardware acceptance remain separate steps.

## 10. Official references (consulted 2026-10-30; recheck before implementation)

- [HA Container requirements](https://www.home-assistant.io/installation/linux/#install-home-assistant-container)
- [Synology installation](https://www.home-assistant.io/installation/alternative)
- [Official Xiaomi integration](https://github.com/XiaoMi/ha_xiaomi_home/blob/main/doc/README_zh.md)
- [Plug 3 power report, not a conclusion for every firmware](https://github.com/XiaoMi/ha_xiaomi_home/issues/1788)
- [HA WebSocket API](https://developers.home-assistant.io/docs/api/websocket/)
- [HA REST API](https://developers.home-assistant.io/docs/api/rest/)
- [Xiaomi BLE](https://www.home-assistant.io/integrations/xiaomi_ble/)
- [ESPHome](https://www.home-assistant.io/integrations/esphome/), [Bluetooth proxy](https://esphome.io/components/bluetooth_proxy/)
- [Scale support](https://esphome.io/components/sensor/xiaomi_miscale/)
- [MQTT](https://www.home-assistant.io/integrations/mqtt/), [REST](https://www.home-assistant.io/integrations/rest/)

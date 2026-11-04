# Initial HA IoT validation record

[中文](README.md)

## Current status

2026-11-04: the unified model, read-only HA adapter, persistence, House API and room device cards are implemented. Initial scope covers a humidifier, thermometer/hygrometer and aquarium, totaling 13 capabilities. The user chose the OnePlus Android TV client for initial acceptance, with TCL validation deferred. The minimum read-only path renders on the OnePlus device; full dynamic and recovery acceptance remains pending.

Candidate `a38fe0f` includes the on-demand cache changes deployed earlier that day. Its backend overlays the verified running release, preserving previous release content and the existing signing identity. Production reached schema 18. Initial background synchronization failed and the original configuration was restored. After approval, isolated system-service validation passed and production switched to that service type, still running as the original ordinary user. Production IoT is enabled with 13/13 capabilities available; Core and both NAS photo sources are healthy. No photos or other NAS originals were copied.

## Verified

- Go `make check` passed, along with race checks for affected IoT, persistence, application and WebSocket paths.
- Final frontend: 88 test files and 661 tests passed, with lint, type checking and build successful. An earlier concurrent run timed out in the full-date lunar test; its isolated rerun and the final full run passed.
- The signed candidate connected to real HA with CA verification using an isolated database and loopback API. All 13 capabilities were available; the public API excluded provider entity identifiers and endpoints. The isolated server exited afterward.
- A separate database copy migrated from schema 17 to 18. Digests of all 74 existing tables were unchanged, integrity/FK checks passed, and the previous release schema guard rejected the new database.
- Synthetic UI checks covered off, valid zero, unknown, unavailable, multiple rooms and directional scrolling, including 804×384. Full 4K visual acceptance remains pending.
- Authorization-clear notices and reconnect boundaries immediately hide cached IoT data, even when the following HTTP request fails. Ordinary updates preserve the rendered snapshot and focus. Regression and queue-coalescing checks passed.

## Permission resolution and remaining acceptance

The same signed candidate connects in the foreground, but TCP connections from an isolated launchd agent repeatedly fail with `no route to host`. A subsequent system Network framework probe returned `nw_path_unsatisfied_reason_local_network_denied` (value 3), confirming the permission rejection. The host runs macOS 15.2. After user approval, an application identity with a usage description was registered, but both background and visible application checks remained denied without an actionable Atrium permission entry. Temporary probes exited and the registered application was restored to the release executable. After approval, a temporary system service running as the original ordinary user passed isolated validation with 13/13 capabilities and was removed. Production then switched to a system launchd daemon, with a non-root runtime user. The original user-agent configuration was backed up and removed from automatic loading to prevent duplicate startup. Production API checks confirmed HA connected, 13/13 capabilities available and both NAS sources online.

After restarting the OnePlus client, the House page displayed HA connected and real readings from all three device types; directional scrolling was verified. No device-control requests were sent. Screenshots remain in a private temporary directory.

Pending: real changes from at least two device classes, ten HA-to-screen latency samples, real disconnect/recovery and HA/host restart acceptance. Static readings, synthetic tests and foreground success do not substitute for these results.

Credentials, actual entities, addresses, databases, raw logs and screenshots remain private and are excluded from this repository.

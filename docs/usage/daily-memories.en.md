# Home memories and multiple photo sources

[中文](daily-memories.md) | English

Updated: 2026-11-04. Status: implemented and deployed, with service, browser and physical OnePlus validation passed. Historical November 2 release evidence is retained below.

## Playback and resume

The Home photo area is named “精彩回忆” (“Memories”). This is a display name; it does not imply AI scoring, face recognition or automated selection of highlights.

- Each household day has a stable random order, using `home.timezone` for its date. Each paired screen has its own order and playback position.
- Photos continue to change every 30 seconds. Reaching the end repeats the same order that day, without reshuffling.
- Returning to Home, reloading the page or restarting the app resumes from the last successfully displayed photo. Fetching, preloading or failing to display a photo does not advance the saved position.
- A new round begins at midnight in the household timezone, including when the app remains open. Returning to Home after being away uses the current day's round.
- The ordinary random collection on Media keeps its existing behavior. It does not use Home's daily order or saved position.

Resume state lives in that screen browser's `localStorage`. It contains only the screen identifier, household date and an opaque cursor for the last successfully displayed item. The cursor includes that item: a reload refetches and displays it before continuing. Resume state does not store images, NAS paths, photo URLs, credentials or an entire library.

Disabling browser storage, clearing site data, changing the origin (protocol, hostname or port), or pairing again loses the saved position. Playback still works when storage is unavailable. Positions do not synchronize between TVs. Pairing again or losing authorization clears old progress; an old cursor cannot restore old authorization.

## Mixing independent sources

Home mixes independent photo sources that are currently active and authorized for display. With the on-demand cache update, metadata-ready indexed photos with regenerable previews participate even after cache eviction; see [on-demand previews and a small cache](photo-cache.en.md). Each source retains its own identity, scan state and revocation state. There are no per-source quotas, so a source with more photos may appear more often. Identical content in two sources counts as two photos; cross-source content deduplication is not included.

Adding or removing photos during the day does not reshuffle the remaining photos. Newly ready photos ranked after the current position can appear later in that day's playback; photos ranked before it may appear in the next loop or on the next day. Initial historical imports can appear on Home and in All Photos, but are treated as a baseline import rather than all being counted as Recently Added.

Each page request and resume request rechecks current authorization and source state. Once a source is revoked, no new photos are fetched from it. A saved cursor is not an access grant and does not retain old authorization.

## Adding another NAS photo directory

Authorize each source to one exact photo subdirectory, rather than an entire share or user directory. Use a stable, read-only SMB mount with `identity.require_mount: true` and `identity.allow_local: false`. These settings prevent a same-named local directory from being accepted when its network share is absent.

The following is a synthetic example. Append the second item to the existing `sources` list; do not replace the full configuration with this fragment. Preserve the existing source's ID, root, identity and scan settings. Source roots must not overlap.

```yaml
sources:
  - id: source_a
    name: "Album A"
    root: "/Volumes/example-media/album-a/Photos"
    include_extensions: ["jpg", "jpeg", "png", "heic", "heif"]
    identity:
      require_mount: true
      allow_local: false
  - id: source_b
    name: "Album B"
    root: "/Volumes/example-media/album-b/Photos"
    include_extensions: ["jpg", "jpeg", "png", "heic", "heif"]
    identity:
      require_mount: true
      allow_local: false
```

Maintainers should verify the mount, directory scope and read permissions, back up the existing configuration, append the independent source and restart Core through the controlled deployment process. Check source identity confirmation, online state, initial scanning and ready metadata, sample on-demand previews, then verify mixed playback on the TV. Manage credentials through the operating system's mount mechanism, keeping them out of public configuration examples and documentation. Originals remain read-only; indexes, the database and generated previews remain on the local disk.

Adding a source does not automatically expand any integration's source allowlist. Explicitly grant access separately if another integration needs it. Do not reset the existing source's identity, clear the library index or relax mount checks to add an album.

## API and delivery boundary

Home uses the screen-authenticated `GET /api/v1/photos/memories` endpoint. The server derives a stable order from household day, screen and photo identifiers, with cursor pagination. Resume requests recheck currently visible photos instead of reusing earlier authorization results. No database schema migration is required.

This release version, test results, source onboarding and device verification should be recorded separately. This guide does not replace release acceptance or promise that every photo will be played within one day.

## 2026-11-04 follow-up validation

The on-demand preview release is deployed. The OnePlus verified mixed playback from both sources, regeneration after eviction, automatic photo changes and reload resume, and was then returned to Archives. The TCL was offline. See the [on-demand release record](photo-cache.en.md#2026-11-04-release-validation) for the version and full validation scope.

## Historical 2026-11-02 release verification

The feature commit is `db99b2c`; the signed Core version is `daily-memories-db99b2c+c06c6a591e9a33c6a93bc0def15d3e29061f9d07`. The database retains its existing 17 migrations. This release uses the verified backend baseline and existing patches, adding only this feature and its matching frontend.

All 627 frontend tests, lint, TypeScript builds and the Chrome 66 style check passed. Full Go regression suites passed for both the repository and the actual release source, along with the memories race check. The signed candidate passed 17 real-server checks with synthetic photos, covering two sources, complete pagination, resume, day changes and server restarts. A temporary verification screen then confirmed the deployed daily order, reload resume, new-source mixing, preview decoding, photo viewing and archive overview. The verification screen was revoked afterward. Existing business data, source identities, photo identities and screen pairings were preserved during activation.

Physical TVs were offline during acceptance, so these results cover the service and browser; physical-device acceptance remains pending. Remaining metadata and previews from the new source's initial import continue through the background queue, and ready items enter the playback pool progressively. Actual household paths, library counts, authentication materials and private acceptance evidence are excluded from the repository.

# On-demand Photo Cache Implementation Plan

> Execute with subagent-driven development, test-first changes and independent review. Work directly in the current checkout as requested; preserve unrelated HA documentation changes. No worktree.

**Goal:** Limit the deployed media cache to 2 GiB, generate photo derivatives only when requested, prepare a bounded number of upcoming slides, and keep daily random selection independent of cached bytes.

**Architecture:** Add `media.preview_mode: eager | on_demand`, retaining eager as the compatibility default. In on-demand mode metadata makes supported pending photos discoverable without generating an entire library. Existing authenticated media requests trigger the guarded preview pipeline. Daily and ordinary random pools include regenerable indexed photos regardless of cache residency, with the same source authorization, failure exclusions, deterministic daily rank and inclusive resume cursors. The TV warms only three upcoming IDs and retains its existing bounded bitmap usage. The cache janitor reclaims derived files using the smaller budget; original NAS files, source identities and database schema remain unchanged.

**Tech Stack:** Go configuration/media/store/HTTP, SQLite schema 17, React/TypeScript, Go tests, Vitest and real-server/browser/device validation.

## 1. Metadata, eligibility and HTTP

Files: `internal/config/{config,load,validate}.go` and tests; `internal/media/extract.go` and pipeline tests; `internal/store/{photoquery,photomedia}.go` and tests; `internal/httpapi/{memories,photos}.go` and tests as needed.

1. Write failing tests for accepted/rejected preview modes and unchanged eager behavior.
2. Write failing real-store/pipeline tests: on-demand metadata publishes only eligible pending rows without `build_preview` jobs; failed/unsupported/excluded/removed rows cannot be resurrected.
3. Implement the mode and guarded metadata publication. Existing media cache misses remain the demand trigger.
4. Write failing tests for random/memories candidates and counts with ready, pending, processing, evicted and unavailable previews. Terminal failure and revoked sources remain excluded. A list request never queues a full page of previews.
5. Implement shared candidate eligibility and remove the ready-preview-only memories filter. Prove cache removal does not alter daily ranks, relative ordering, pagination or inclusive resume.
6. Give explicitly requested on-demand previews priority over background metadata work (`internal/jobs/queue.go` and media-request tests), keeping eager background work at its existing priority. Verify that an import backlog cannot indefinitely starve a visible cold photo.
7. Run focused config, media, store and HTTP tests before the full suite.

## 2. Cache eviction

Files: `internal/media/janitor.go`, `internal/media/janitor_test.go`, supporting store tests only where necessary.

1. Reproduce thumbnail-only eviction incorrectly marking the entire photo evicted.
2. Keep main-preview status when only a thumbnail is removed; actual preview eviction leaves the indexed photo eligible for regeneration.
3. Preserve cache bookkeeping if physical removal fails. Verify a quota reduction reclaims enough bytes, including multiple eviction batches, while retaining photos and authorization.
4. Verify cache misses regenerate through the normal bounded worker pipeline, with temporary files cleaned up and NAS originals unchanged.

## 3. TV selection and bounded preparation

Files: `web/src/app/slideshow.ts`, `web/src/widgets/useSlideshow.ts`, their regression tests and relevant mock fixtures.

1. Write failing tests for a cold first photo (`202` then ready), inclusion of regenerable statuses, and progress saved only after actual display.
2. Implement daily candidate selection without treating cache residency as availability.
3. Warm only the next three IDs using ranged probes; retry processing responses finitely and retain the current image while a new image is prepared. Do not generate all metadata-page items or decode multiple additional bitmaps.
4. Verify no per-tick request loop, terminal failures, generation/day changes, authorization purge, source revocation, unmount and StrictMode replay. Preserve per-screen resume and same-day order.
5. Run focused tests, then full frontend tests, lint and TypeScript/Vite/Chrome 66 build checks once code is stable.

## 4. Independent review and documentation

Review backend authorization/eligibility and cache bounds independently from frontend race/prefetch behavior. Maintain Chinese and English usage/status documentation with synthetic examples; never commit private NAS paths, household data, credentials or screenshots. Record implemented capabilities separately from actual release and device verification.

## 5. Release, reclaim and verify

1. Prepare a private source archive from the known deployed backend baseline plus its existing explicit patches. Apply only reviewed new backend changes and the pinned frontend build; run compatibility regression and race checks.
2. Sign with the existing identity, verify the release manifest, and run a synthetic real-server fixture covering cold metadata without eager generation, bounded demand generation, source mixing, eviction, pagination and resume.
3. Prepare a private configuration changing only the media cache quota to `2147483648` and preview mode to `on_demand`.
4. Back up database, binary and configuration; preserve business records, source/photo identities and paired screens. Deploy once with rollback of binary/configuration while retaining the latest database. Removed derived cache files are regenerable and are not restored from backup.
5. Verify the running version, signatures, schema, source health, a full-library daily pool and bounded cache bytes. Let the normal janitor reclaim excess derived files; never delete NAS originals or reset the index.
6. Verify actual cold-photo generation and successful OnePlus playback/resume, restoring its original page afterward. Report TCL or other unavailable device checks separately.
7. Commit the reviewed implementation and bilingual verification records, integrate and push main following the user's existing delivery preference; preserve unrelated uncommitted files.

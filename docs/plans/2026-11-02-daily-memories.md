# Daily Memories Implementation Plan

> Execution: subagent-driven development with test-first implementation and an independent review.

> Workspace: develop directly in the current checkout, following the user's instruction not to use a worktree; preserve unrelated local changes.

**Goal:** Home is “精彩回忆”: a daily random order in the household timezone, per-screen resume after reload, and mixed independent authorized photo roots.

**Architecture:** Add an authenticated, read-only `/api/v1/photos/memories` endpoint. Derive a stable photo rank from household day, authenticated screen ID and photo ID; keyset cursors survive source additions/removals without reshuffling existing photos. Each item provides an inclusive resume cursor; the page cursor advances exclusively. Persist only the last displayed cursor/day/screen ID in local browser storage; reload refetches authorized metadata and never persists pixels, URLs, credentials or a whole library. No database migration. Ordinary Media collections retain their current behavior.

**Tech Stack:** Go HTTP/store, React/TypeScript reducer/effects, localStorage, Vitest and Go tests. Chrome 66 remains the browser baseline.

## Task 1: Daily page contract and backend

Files: create `internal/httpapi/memories.go` and `memories_test.go`; modify `internal/httpapi/routes.go` only to register the screen-authenticated route.

Contract: `{items:[{...PhotoItem,resume_cursor:string}],next_cursor:string|null,meta:{collection:'memories',day:string,screen_id:string,total:number}}`. Limits match photo list (max100), bounded pool100000, ready photos/previews and active sources only. Rank is deterministic for one day/screen, stable under membership changes, with photo ID tie-breaker. Cursor includes version/day/screen and anchor photo ID plus inclusive/exclusive mode; validate size/shape. A prior-day cursor starts today from the beginning; cross-screen cursor is rejected. Source revocation/removal must be re-evaluated on each page. Cache-Control no-store. Existing random endpoints are unchanged.

Write and observe failing tests first: repeatable same-day paging, day rollover in household timezone, per-screen cursor isolation, resume exactly current item, resume after exclusion/revoke, new sources mixed, stable existing order after additions, malformed cursor/limit, unauth/revoked auth. Run `go test -p 1 ./internal/httpapi -run Memories` and existing photo/auth tests.

## Task 2: Resume storage, daily driver and label

Files: `web/src/types/api.ts`, `core/api.ts`, new `app/memoriesProgress.ts` with tests; slideshow reducer/hook, refetchers, AppProvider auth cleanup, PhotoPane and relevant tests/mock fixtures.

Use the endpoint contract above. Daily page metadata identifies owner/day; app clock detects household midnight even without a page request. Persist only successfully displayed item cursor (not preloaded/fetched/failed items). Restore using the inclusive cursor; stale-day response replaces the old round. A day repeats the same order if exhausted; never reseed during the day. Tab changes pause and preserve progress; restarting refetches and resumes; each screen stores its own position. Revocation/expiry/re-pairing must clear saved progress and ignore old in-flight results. Invalid/quota/disabled storage degrades to working playback. Refresh and `data.changed` resume around current anchor rather than restart at page one. Label the Home photo area “精彩回忆”; keep30-second cadence.

Write and observe failing Vitest tests first for reload/resume, midnight, independent devices, auth cleanup/stale responses, storage failures, source mutations, finite empty/single/failed-photo behavior, existing media navigation and remote controls. Run focused tests before the full frontend suite, lint, typecheck/build and Chrome66 guard.

## Task 3: Documentation and review

Add Chinese/English usage documentation with synthetic identities and placeholder NAS paths; update README language pair consistently. Private source configuration and actual household media/paths never enter Git. Independent review focuses on auth boundaries, progress races, daily/date/cursor correctness, boundedness and Chrome66.

## Task 4: Deploy and source onboarding

Reuse the known c06 release baseline plus explicit prior LivePhoto/archive/growth patches, adding only the reviewed memories backend files and pinned frontend bundle. Validate candidate build/source compatibility, original signing identity, health, media/archives and backup/rollback. Preserve existing private config blocks and grants; append a separate photo-only source rooted at the exact second-person Photos directory on a read-only SMB mount. Keep existing source identity and integration allowlists. Restart Core once, verify both sources, initial scan and previews. Original NAS files stay read-only. Verify daily resume and both-source playback on available TV devices; report unavailable physical-device validation distinctly. Commit implementation/docs and integrate after checks, leaving unrelated user work intact.

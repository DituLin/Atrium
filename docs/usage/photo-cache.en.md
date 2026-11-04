# On-demand photo previews and a small cache

[中文](photo-cache.md) | English

Updated: 2026-11-04. Status: implemented with code validation complete; deployment acceptance remains pending for this release.

## Storage behavior

Original photos remain on the NAS. The Mac stores the index, metadata, and JPEG previews and thumbnails generated for playback and browsing. On-demand mode does not build previews for an entire newly imported library. After metadata is ready, a photo can become a playback candidate; its first media request runs the normal guarded decode pipeline to produce derivatives.

Home derives its daily order from displayable indexed photos whose sources remain authorized. Evicting cached files does not remove photos from the random pool. Unsupported photos, permanent preview failures, excluded or removed photos, and revoked sources remain filtered. Sources are mixed by photo rather than assigned equal quotas.

Home prepares the current photo and at most three upcoming photos. Fetching a metadata page does not generate that whole page. The Media grid prepares thumbnails according to viewport visibility. Resume progress is still saved only after a photo is displayed successfully; preloading does not advance it. When the NAS is offline, existing cached images remain usable and photos without previews are temporarily skipped.

## Configuration

Merge this fragment into the existing configuration without replacing sources, archive settings or authorization policies:

```yaml
storage:
  cache_budget_bytes: 2147483648 # 2 GiB, approximately 2.15 GB
media:
  preview_mode: on_demand
```

`preview_mode` accepts `eager` or `on_demand`. The default `eager` preserves batch generation after metadata extraction. `on_demand` generates previews when playback or browsing requests them. This changes when derivatives are prepared and does not mirror the NAS original library onto the Mac.

The budget covers photo previews, thumbnails, and video covers when enabled. Databases, archive originals, logs, backups, release packages and temporary files are separate. Cleanup removes less recently accessed files and reclaims the photo partition to approximately 90% when it exceeds its quota. Startup also checks a reduced quota. Concurrent writes and temporary conversion can briefly increase disk usage; this is not a hard limit for the entire application.

## Operation and verification

After a controlled configuration change and Core restart, verify unchanged source identities and screen pairings, preserved indexed photos, and background reclamation of excess derivative files. There is no need to reset the library index or reauthorize sources. Removing a thumbnail must not make a retained main preview appear unavailable. Removing the main preview must still allow on-demand regeneration.

A first generation can be slower than a cache hit, particularly for HEIC conversion or a slow NAS. Acceptance should cover a cold-cache first image, preparation of the next image, random order and resume after eviction, source revocation, cache usage and actual TV playback. Original-photo directories remain read-only.

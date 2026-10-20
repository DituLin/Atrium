# Live Photo classification deployment verification

[中文](README.md) | English

Fix `96b093ac57667fbb0a6642f0e8d1e8511791ae4e` was deployed on 2026-10-20. The backend baseline remains `c06c6a591e9a33c6a93bc0def15d3e29061f9d07`, with frontend `041b4833678109735c5799de6d1f65ac88b11067`. The running version is `live-photo-96b093a+c06c6a591e9a33c6a93bc0def15d3e29061f9d07`.

- The Apple content identifier signals a Live Photo companion; only a boolean classification is persisted, without retaining the Apple identifier. List, detail, cover, ticket and content routes share the filter, while still photos remain available. Ordinary MOV, MP4 and short videos are not excluded by extension or duration.
- Existing ready/unsupported entries without classification are automatically reprobed. Classification is fenced by file revision, source authorization generation and expiring work leases. Reauthorization, retry backoff and interrupted processes recover correctly. Confirmed Live Photos need no video cover and are not repeatedly claimed.
- `go test -p 1 ./...` passed, as did the four media-package race checks and final added reauthorization/retry-recovery/HTTP-filter race regressions. An existing native output-limit test timed out during the initial parallel full run; the serial full rerun passed without changing that test.
- Only the 13 explicit source/test patches were applied to the deployed backend baseline, and the five related packages passed. An isolated synthetic old-index upgrade started with two videos: the candidate identified one Live Photo while retaining the ordinary one-second MOV and one still photo. Embedded frontend hashes matched.
- The existing signing identity was reused; strict signature and designated requirement checks matched. Of 58 manifest entries, only Core and provenance changed; the other 56 entries remained byte-identical. Schema remains 17.
- Ingress and Core were stopped for a consistent private backup, with SQLite integrity/FK checks passing before the atomic Core replacement. Health, configuration, photo index, household/archive data and source identity checks passed; the NAS is online.
- Runtime checks returned 404 for four video-route classes of a classified Live Photo. An ordinary short video's detail returned 200 and an actual Range read returned 206. Legitimate-screen tickets and rejection of old tickets are covered by HTTP regression tests. OnePlus reconnected and applied the Archives navigation command.

Full reclassification of existing entries is still running in the background and is not recorded as complete here. These results do not replace physical TCL TV, reboot, sustained-stability or complete T15–T18 acceptance. Private backups, real media and host paths are excluded from the repository.

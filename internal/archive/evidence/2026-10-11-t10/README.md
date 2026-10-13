# T10 structured archive search verification

Verified on 2026-10-11 in the isolated AtriumOS worktree. These logs cover the current structured search, entity lookup, evidence APIs and schema 15. All final commands below terminated with exit code 0 before this record was written.

| Command | Evidence | Result |
| --- | --- | --- |
| `go test ./... -count=1` | `full-go.log` | All packages passed; store 8.049s |
| `go test -race ./internal/store -run 'TestArchiveSearch|TestArchiveReadSnapshot' -count=1` | `search-race.log` | Passed, 20.159s |
| `go test -race ./internal/store ./internal/httpapi ./internal/app ./internal/archiveworker -count=1` | `affected-race.log` | Passed; store 125.625s, HTTP 61.639s, app 17.951s, worker 4.695s |
| `GOOS=linux GOARCH=amd64 go build -o /tmp/atriumos-t10-linux ./cmd/atrium` | `linux-build.log` | Passed; empty output is expected |

The reproducible synthetic suite in `internal/store/archive_search_test.go` includes 32 structured questions across health, education, finance and property. It also verifies literal queries, bounded serialized output, signed pagination, current credential and screen revocation, private and withdrawn records, current legacy corrections, same names and aliases, multiple owners, date precision, multiple source receipts, protected identity ranges, current references to older extractions, partial indexing, rebuild generations and read snapshots that permit concurrent writers. HTTP tests exercise the route boundaries and reject unsupported or duplicate filters.

`initial-race-diagnostic.log` is a retained, superseded failing intermediate run, not acceptance evidence. It exposed SQL NULL conversion for a missing partial FTS row and a test fixture that assumed a time-bounded indexing batch completed all rows. The implementation now coalesces the missing-row predicate, and readiness fixtures drain bounded batches; a deterministic partial-index regression test is included. The final race logs above supersede that diagnostic.

Scope and limits: this is T10 synthetic structured API verification. It is not the later 42-question real-model/IM/native/TV end-to-end acceptance, a production deployment, a replay of real household files, or a 10,000-document latency benchmark. No new provider calls or outbound messages were made. Runtime limits and incomplete-coverage behavior are documented in `internal/archive/README.search.md`; further natural-language wiring, analysis, UI and deployment belong to subsequent tasks.

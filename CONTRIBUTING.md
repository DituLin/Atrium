# Contributing to Atrium

Atrium is a local-first home hub: a single Go binary that serves an embedded web
UI to a TV and reads a NAS photo share read-only. Contributions must keep those
properties intact.

## Ground rules

- Code, comments, identifiers, commit messages and documentation are written in
  English.
- The technical design (`docs/tech/2026-09/atrium-home-hub/tech-design.md`) is the
  authority for contracts. When code and design disagree, follow the design and
  record the deviation in `docs/plan/2026-09/atrium-home-hub/dev-plan.md` §11.
- No real NAS paths, IP addresses, hostnames, emails or photos in the repository.
  Examples use `192.168.1.10`, `/Volumes/photos/family`, `living_room_tv`.
- Never commit `web/dist` content, data directories, tokens or certificates.

## Toolchain

- Go 1.25 or newer (`go build ./...` must work without cgo).
- Node 20 or newer for the web client.
- `golangci-lint` and `shellcheck` for linting.

## Workflow

```
make build     # compile the binary into bin/atrium
make web       # build the web client into web/dist
make test      # go test ./...
make lint      # go vet + golangci-lint
make check     # lint + test + build
make run-dev   # run a development server with console logging
```

A change is complete when `make check` is green and the new behaviour has tests.
Tests live next to the code they cover; they use temporary directories,
`httptest` servers and the fake clock rather than network or real hardware.

## Go style

- `gofmt`; errors wrapped with `%w`; a `context.Context` on every I/O boundary.
- No mutable package-level state outside `main`.
- Repositories own all SQL; nothing outside `internal/store` writes queries.
- Never log tokens, cookies, the `Authorization` header, NAS `root_path`,
  `rel_path` or `mount_from` at `info` level or above.

## Commits

One task ID per commit where practical:

```
feat(auth): B-102 auth middleware and scopes
```

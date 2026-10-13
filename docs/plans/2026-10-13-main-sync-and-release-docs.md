# Main sync and bilingual release documentation implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development for independent fixes and fresh reviews. Steps use checkbox syntax for tracking.

**Goal:** Preserve the currently deployed AtriumOS code in `main`, restore CI readiness, and publish Chinese/English project and deployment documentation.

**Architecture:** Keep the deployed commit `c06c6a591e9a33c6a93bc0def15d3e29061f9d07` in main's history through a merge PR. Public GitHub-hosted jobs produce credential-free candidates; independent controlled deployment verifies the exact source/artifact and uses the existing signing identity on the production Mac. Documentation describes this agreed boundary and distinguishes implemented steps from automation still to be connected.

**Tech stack:** Go, TypeScript/React, ShellCheck, GitHub Actions, macOS arm64, launchd, SQLite, Markdown.

## 1. Verify the source and fix the merge gate

- [x] Verify HTTPS readiness returns the deployed commit; refresh remote refs and confirm `origin/main` is an ancestor of `origin/codex/atriumos`.
- [x] Read the actual failed CI run: Go lint, Web lint and ShellCheck failed before downstream tests/builds.
- [x] Fix only the reported Go source/test diagnostics and Web/Shell issues; do not lower lint rules. Reproduce meaningful error-propagation changes with regression tests first.
- [x] Run the same checks as CI, inspect a fresh review, then commit and push the fixes.

Validation commands (repository root, except Web commands):

```sh
git diff --check
golangci-lint run
go vet ./...
go test -race ./...
go build ./...
python3 -m unittest discover -s deploy/release -p 'test_*.py'
```

```sh
cd web
npm run lint
npm test
npm run build
```

Run ShellCheck on every path returned by `git ls-files '*.sh'`, passing paths as arguments without evaluating their contents.

## 2. Merge deployed code into main

- [x] Create a PR from `codex/atriumos` to `main` with the final validation and pre-release limits documented.
- [x] Attach the PR to this task and require the actual GitHub checks to pass.
- [x] Merge using a merge commit so the deployed source remains in history. Do not squash/rebase away the deployed commit identity.
- [x] Refresh refs and verify:

```sh
git fetch origin
git merge-base --is-ancestor c06c6a591e9a33c6a93bc0def15d3e29061f9d07 origin/main
```

Keep old branches/worktrees until they are no longer needed; this task does not delete historical work.

## 3. Add Chinese/English project and release documentation

- [x] Make `README.md` the Chinese primary entry and keep a complete `README.en.md`, with mutual language links and matching technical facts/commands.
- [x] Add paired release/deployment documents under `docs/ops/`, covering branch/tag policy, current CI, candidate packaging, artifact transfer, local signing, operational backups, deployment checks and schema-safe recovery.
- [x] Record the language policy for future project documentation and link the new guides from the existing deployment entry.
- [x] Describe current delivery honestly: local archive/TV release is implemented, while complete AgentOS and unattended release acceptance remain separate work. Do not describe proposed workflows/controllers as installed.
- [x] Check Markdown links, command validity against code, bilingual consistency and absence of real family data/secrets. Run a fresh reader/review agent.
- **Final GitHub delivery gate:** commit, push and merge the documentation PR into `main`, then verify remote main contains the delivered files. The follow-up PR records this final merge outcome.

## Acceptance

- `main` contains the verified deployed commit and passes current CI.
- Chinese and English README/release guides are discoverable and consistent.
- The public build/private deployment/local signing boundary is explicit and actionable.
- No family data, tokens, signing keys, live configuration or private screenshots are added to GitHub.
- Main synchronization does not constitute a new production rollout or completion of the remaining AgentOS acceptance tasks.

## Local merge-gate verification (2026-10-13)

Fresh checks passed: all Go race tests, vet/build, gofmt, golangci-lint v2.14.0 (0 issues), Web lint and 471 tests plus build/type checks, all 13 ShellCheck scripts, and two release-tool unit tests. An independent review found no actionable findings in the CI fix diff, including maintenance error preservation, wrapped errors and fixture path validation. GitHub checks and the merge remain the next gates.

## Source integration and document verification (2026-10-13)

PR #4 merged as `4ad689c1e013c4a2015b5f90a1686b593cbcb7cf` after GitHub push and PR workflows passed Go, Web, Shell and Release binaries. Both the deployed `c06c6a5` and CI-fix `61db776` are ancestors of remote main. The original primary checkout has been fast-forwarded; historical branches/worktrees are preserved.

Paired README/release guides passed independent reader review. Follow-up clarification covers runtime binding fields and verification limits, maintenance schema/backfill responsibilities, and stopping unsupported rollback. Final paired shell blocks and inline technical identifiers match; relative links resolve against the repository. Documentation merge status is recorded by its follow-up GitHub PR.

## CI follow-up discovered during documentation delivery (2026-10-13)

Both workflows for documentation-only commit `32ceef7` exposed pre-existing timing failures despite the source merge workflows passing. Ordinary archive HTTP fixtures used a 150 ms upload deadline for real SQLite/blob work, causing request-timeout responses to preempt envelope or size assertions on slower race runners. Ordinary fixtures now use a separate deadline; the dedicated blocked-socket case retains its original 150 ms deadline and 408/time/zero-row assertions.

A deterministic WebSocket transport gate reproduced the protocol-close race in the original implementation (10/10 failures): the client can receive `session.ready` while its server Write is still returning, so cancellation closes TCP before the selected 4005 frame. A reviewed bounded close path and actual TLS partial-frame regressions are included before the documentation PR is merged; neither CI rules nor protocol assertions are disabled. This follow-up changes source as well as documentation and does not deploy a new runtime.

Final local follow-up checks passed: full Go vet/race/build, global golangci-lint v2.14.0 (0 issues), HTTP affected cases repeated five times with the dedicated timeout unchanged, normal WebSocket/upgrade cases repeated twenty times, and actual partial TLS frames finishing HTTP/Hub cleanup in about 5.7 seconds. The independent final review found no actionable findings. The new five-second budget applies after readLoop exits; normal writeTimeout remains ten seconds and no general guarantee is added for unrelated synchronous hooks. GitHub will validate the final follow-up commit before merge.

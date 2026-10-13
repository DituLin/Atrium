# T11 verification evidence

All data in this directory is synthetic. No credential, ordinary household
message or production Core response is included.

- `two-domain-query.json`: exact successful `ask.mjs --json` result from a real
  isolated OpenClaw/DeepSeek invocation against a temporary TLS Core. Model
  calls: education + 数学 and health + 血红蛋白. Both actual tool receipts are
  retained. Elapsed 9,434 ms; failed=false; timed_out=false. The synthetic fixture
  was parsed, validated, published and indexed before the model queried it.
- `two-domain-rendered.txt`: the same captured receipts rendered with the final
  human-readable source format. Source R.D.E maps directly to receipt R, document
  item D, evidence E. No new model call or substituted result was used.
- `full-go.log`: final `go test -p 1 ./...`, exit 0.
- `race.log`: `go test -race -p 1 ./internal/brain ./internal/brainhost
  ./internal/homemcp ./internal/integration ./internal/httpapi ./internal/store`,
  exit 0. Store completed in 130.549 seconds; all six packages passed.
- `node.log`: final `node --test adapters/openclaw/*.test.mjs`, 62/62 pass.
- `linux-build.log`: `GOOS=linux GOARCH=amd64 go build ./...`, exit 0 (no output).
- `initial-full-go-diagnostic.log`: retained unsuccessful parallel diagnostic.
  Two old inventory assertions expected 13 rather than 20 and were updated while
  preserving their functional checks. The pre-existing native flood test hit
  its 3-second test budget under parallel load; its isolated rerun passed in
  0.822 seconds, followed by the full serial suite. No parser code was changed.

Additional targeted results: real Core seven-tool test passed in 0.613 seconds;
MCP/Brain targeted contract tests passed. The final full suite includes them.

This is T11 evidence only. It does not claim T12 analysis, real IM delivery,
TV rendering, NAS recovery, full 42-query acceptance or production deployment.

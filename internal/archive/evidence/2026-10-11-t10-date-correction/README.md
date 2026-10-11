# T10 review correction: effective dates

The independent review found that a visible manual value assertion was searchable as text while date filters still used the original normalization. The unchanged reviewer overlay was reproduced RED (`review-red.log`) before editing. The original overlay and test source are retained verbatim as `original-review-overlay.json` and `original-review-test.go.txt`; the overlay paths refer to the reviewer's original temporary files. The same original overlay command passed after the fix (`review-green.log`).

`TestArchiveSearchCorrectedDatePrecision` first failed (`boundary-red.log`) and now passes (`boundary-green.log`). It covers valid day/month/year assertions; invalid calendar dates/months; short or missing years; unknown, empty, and noncanonical values; evidence retaining original normalization and current assertion; and public versus private field projections. Partial dates never manufacture a day. Invalid assertions do not fall back to the old source date and set review-required/incomplete coverage.

Final commands, all exit 0:

- `go test -overlay /tmp/t10-independent-spec-overlay.json ./internal/store -run TestReviewCorrectedDateSearch -count=1`
- `go test ./internal/store -run TestArchiveSearchCorrectedDatePrecision -count=1`
- `go test -race ./internal/store -run 'TestArchiveSearch|TestArchiveReadSnapshot' -count=1` — 28.941s, `search-race.log`.
- `go test ./... -count=1` — all packages passed, store 7.901s, `full-go.log`.
- `GOOS=linux GOARCH=amd64 go build -o /tmp/atriumos-t10-date-linux ./cmd/atrium` — `linux-build.log`, empty output on success.

`parallel-full-go-diagnostic.log` retains an earlier failed full run executed concurrently with race: the existing native parser flood fixture hit its 3-second parser timeout instead of its output limit. After the race completed, the identical full Go command passed. No parser or timeout code was changed. This diagnostic is not acceptance evidence.

No production data, provider calls, outbound messages, T11 changes, real-model/IM/TV acceptance or performance claims are included.

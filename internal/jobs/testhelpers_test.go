package jobs_test

import (
	"io"
	"log/slog"
)

// quiet keeps expected worker warnings out of the test output.
func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

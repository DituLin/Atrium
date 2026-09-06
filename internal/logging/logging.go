package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Options configures the logger built by New.
type Options struct {
	// Level is one of debug, info, warn, error.
	Level string
	// Dir receives atrium.log; empty disables file logging.
	Dir string
	// Console adds a human-readable handler on stderr (development).
	Console bool
	// RetainDays and MaxTotalMB implement the retention rules of design §6.11.
	RetainDays int
	MaxTotalMB int
	// Extra handlers receive every record alongside the files. Diagnostics
	// uses one to keep the recent-errors ring without every component having
	// to know that diagnostics exist.
	Extra []slog.Handler
}

// Logger is a configured slog logger with the resources it owns.
type Logger struct {
	*slog.Logger
	closers []io.Closer
}

// Close releases the log files.
func (l *Logger) Close() error {
	var firstErr error
	for _, c := range l.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ParseLevel maps a configuration string to a slog level.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LogFileName is the rotating log file inside the log directory.
const LogFileName = "atrium.log"

// maxSizeMB is the per-file rotation threshold from design §6.11.
const maxSizeMB = 20

// New builds a logger writing JSON lines to the log directory and, when
// requested, a readable stream on stderr.
func New(opts Options) (*Logger, error) {
	level := ParseLevel(opts.Level)
	handlerOpts := &slog.HandlerOptions{Level: level, ReplaceAttr: Redact(level)}

	var handlers []slog.Handler
	var closers []io.Closer

	if opts.Dir != "" {
		if err := os.MkdirAll(opts.Dir, 0o700); err != nil {
			return nil, fmt.Errorf("logging: create %s: %w", opts.Dir, err)
		}
		backups := opts.MaxTotalMB / maxSizeMB
		if backups < 1 {
			backups = 1
		}
		rotator := &lumberjack.Logger{
			Filename:   filepath.Join(opts.Dir, LogFileName),
			MaxSize:    maxSizeMB,
			MaxAge:     opts.RetainDays,
			MaxBackups: backups,
			Compress:   false,
			LocalTime:  true,
		}
		closers = append(closers, rotator)
		handlers = append(handlers, slog.NewJSONHandler(rotator, handlerOpts))
	}
	if opts.Console || len(handlers) == 0 {
		handlers = append(handlers, slog.NewTextHandler(os.Stderr, handlerOpts))
	}
	handlers = append(handlers, opts.Extra...)

	var h slog.Handler
	if len(handlers) == 1 {
		h = handlers[0]
	} else {
		h = &fanout{handlers: handlers}
	}
	return &Logger{Logger: slog.New(h), closers: closers}, nil
}

// Discard returns a logger that writes nowhere; tests use it.
func Discard() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

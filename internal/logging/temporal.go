package logging

import (
	"fmt"
	"log/slog"

	"go.temporal.io/sdk/log"
)

// TemporalLoggerAdapter adapts an slog.Logger to the Temporal SDK's log.Logger interface.
type TemporalLoggerAdapter struct {
	logger *slog.Logger
}

// Ensure TemporalLoggerAdapter satisfies log.Logger.
var _ log.Logger = (*TemporalLoggerAdapter)(nil)

// NewTemporalLoggerAdapter creates a new TemporalLoggerAdapter wrapping the provided slog.Logger.
func NewTemporalLoggerAdapter(l *slog.Logger) *TemporalLoggerAdapter {
	if l == nil {
		l = slog.Default()
	}
	return &TemporalLoggerAdapter{logger: l}
}

func (a *TemporalLoggerAdapter) Debug(msg string, keyvals ...any) {
	a.logger.Debug(msg, toSlogArgs(keyvals)...)
}

func (a *TemporalLoggerAdapter) Info(msg string, keyvals ...any) {
	a.logger.Info(msg, toSlogArgs(keyvals)...)
}

func (a *TemporalLoggerAdapter) Warn(msg string, keyvals ...any) {
	a.logger.Warn(msg, toSlogArgs(keyvals)...)
}

func (a *TemporalLoggerAdapter) Error(msg string, keyvals ...any) {
	a.logger.Error(msg, toSlogArgs(keyvals)...)
}

func toSlogArgs(keyvals []any) []any {
	if len(keyvals) == 0 {
		return nil
	}
	args := make([]any, 0, len(keyvals))
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			key, ok := keyvals[i].(string)
			if !ok {
				key = fmt.Sprintf("%v", keyvals[i])
			}
			args = append(args, slog.Any(key, keyvals[i+1]))
		} else {
			// Odd number of keyvals, add as dangling value
			args = append(args, slog.Any("EXTRA_VALUE", keyvals[i]))
		}
	}
	return args
}

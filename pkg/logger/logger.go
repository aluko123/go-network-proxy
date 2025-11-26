package logger

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey string

const (
	RequestIDKey ctxKey = "request_id"
	loggerKey    ctxKey = "logger"
)

type Logger struct {
	*slog.Logger
}

func New(format string) *Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return &Logger{slog.New(handler)}
}

func (l *Logger) With(key string, val any) *Logger {
	return &Logger{l.Logger.With(key, val)}
}

func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(loggerKey).(*Logger); ok {
		return l
	}
	return New("json")
}

func WithContext(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

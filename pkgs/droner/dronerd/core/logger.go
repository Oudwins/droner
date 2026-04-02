package core

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lmittmann/tint"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
)

type fanoutHandler struct {
	handlers []slog.Handler
}

func newFanoutHandler(handlers ...slog.Handler) slog.Handler {
	cloned := make([]slog.Handler, 0, len(handlers))
	for _, handler := range handlers {
		if handler != nil {
			cloned = append(cloned, handler)
		}
	}

	return &fanoutHandler{handlers: cloned}
}

func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}

	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	var err error
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, record.Level) {
			continue
		}

		err = errors.Join(err, handler.Handle(ctx, record.Clone()))
	}

	return err
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithAttrs(attrs))
	}

	return &fanoutHandler{handlers: handlers}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithGroup(name))
	}

	return &fanoutHandler{handlers: handlers}
}

func InitLogger(runtimeEnv *env.EnvStruct) (*slog.Logger, *os.File) {
	var logFile *os.File
	level := runtimeEnv.LOG_LEVEL.SlogLevel()
	handlers := make([]slog.Handler, 0, 2)
	if runtimeEnv.LOG_OUTPUT == env.LogOutputStd || runtimeEnv.LOG_OUTPUT == env.LogOutputBoth {
		handlers = append(handlers, tint.NewHandler(os.Stdout, &tint.Options{
			Level:     level,
			AddSource: true,
		}))
	}
	if runtimeEnv.LOG_OUTPUT == env.LogOutputFile || runtimeEnv.LOG_OUTPUT == env.LogOutputBoth {
		logPath := filepath.Join(runtimeEnv.DATA_DIR, "log.txt")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			assert.AssertNil(err, "[CORE] Failed to initialize log directory")
		}
		var err error
		logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		assert.AssertNil(err, "[CORE] Failed to open log file")

		handlers = append(handlers, slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level:     level,
			AddSource: true,
		}))
	}

	logger := slog.New(newFanoutHandler(handlers...))

	slog.SetDefault(logger)
	return logger, logFile
}

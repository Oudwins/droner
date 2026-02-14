package core

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lmittmann/tint"

	"github.com/Oudwins/droner/pkgs/droner/internals/assert"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func InitLogger(config *conf.Config) (*slog.Logger, *os.File) {
	logPath := filepath.Join(config.Server.DataDir, "log.txt")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		assert.AssertNil(err, "[CORE] Failed to initialize log directory")
	}
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	assert.AssertNil(err, "[CORE] Failed to open log file")
	logWriter := io.MultiWriter(os.Stdout, logFile)
	handler := tint.NewHandler(logWriter, &tint.Options{
		Level:     slog.LevelDebug,
		AddSource: true,
	})
	logger := slog.New(handler)

	slog.SetDefault(logger)
	return logger, logFile
}

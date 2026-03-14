package applog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	loggerpkg "github.com/simp-lee/logger"

	"github.com/simp-lee/isdict-api/internal/config"
)

type ManagedLogger interface {
	With(args ...any) *slog.Logger
	Sync() error
	Close() error
}

func NewBootstrap() (ManagedLogger, error) {
	return loggerpkg.New(
		loggerpkg.WithLevel(slog.LevelInfo),
		loggerpkg.WithConsole(true),
		loggerpkg.WithConsoleWriter(os.Stdout),
		loggerpkg.WithConsoleFormat(loggerpkg.FormatJSON),
		loggerpkg.WithConsoleColor(false),
	)
}

func ConfiguredOptions(cfg *config.Config) []loggerpkg.Option {
	options := []loggerpkg.Option{
		loggerpkg.WithLevel(ParseLevel(cfg.LogLevel)),
		loggerpkg.WithAddSource(cfg.GinMode == gin.DebugMode),
	}

	if NormalizeOutput(cfg.LogOutput) == "file" {
		options = append(options,
			loggerpkg.WithConsole(false),
			loggerpkg.WithFile(true),
			loggerpkg.WithFilePath(cfg.LogFilePath),
			loggerpkg.WithFileFormat(loggerpkg.FormatJSON),
			loggerpkg.WithMaxSizeMB(cfg.LogMaxSizeMB),
			loggerpkg.WithRetentionDays(cfg.LogMaxAgeDays),
			loggerpkg.WithMaxBackups(cfg.LogMaxBackups),
		)
	} else {
		options = append(options,
			loggerpkg.WithConsole(true),
			loggerpkg.WithConsoleWriter(os.Stdout),
			loggerpkg.WithConsoleFormat(loggerpkg.FormatJSON),
			loggerpkg.WithConsoleColor(false),
		)
	}

	return options
}

func NewConfigured(cfg *config.Config) (ManagedLogger, error) {
	return loggerpkg.New(ConfiguredOptions(cfg)...)
}

func Cleanup(log ManagedLogger, fallback io.Writer) {
	if err := log.Sync(); err != nil {
		_, _ = fmt.Fprintf(fallback, "failed to sync logger: %v\n", err)
	}
	if err := log.Close(); err != nil {
		_, _ = fmt.Fprintf(fallback, "failed to close logger: %v\n", err)
	}
}

func ParseLevel(level string) slog.Level {
	switch NormalizeLevel(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func NormalizeLevel(level string) string {
	return strings.ToLower(strings.TrimSpace(level))
}

func NormalizeOutput(output string) string {
	normalized := strings.ToLower(strings.TrimSpace(output))
	if normalized == "" {
		return "stdout"
	}
	return normalized
}

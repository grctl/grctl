package srvlog

import (
	"io"
	"log/slog"
	"os"
)

type Config struct {
	Level     string
	Format    string
	AddSource bool
}

func Init(cfg Config) {
	initWithWriter(os.Stdout, cfg)
}

func initWithWriter(w io.Writer, cfg Config) {
	level := parseLevel(cfg.Level)

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource || level <= slog.LevelDebug,
	}

	format := cfg.Format
	if format == "" {
		format = detectFormat()
	}

	var h slog.Handler
	switch format {
	case "text":
		h = newTextHandler(w, opts)
	default:
		h = slog.NewJSONHandler(w, opts)
	}

	slog.SetDefault(slog.New(h))
}

func detectFormat() string {
	if isTerminal() {
		return "text"
	}
	return "json"
}

func isTerminal() bool {
	stat, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

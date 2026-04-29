package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "grctld",
	Short: "grctl Server Daemon",
	Long: `grctl is a lightweight workflow execution engine built on NATS.io.

grctld runs the embedded NATS broker and handles workflow execution.`,
	SilenceUsage: true,
	RunE:         runServer,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("grctld %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
	},
}

var (
	configPath   string
	logLevel     string
	startPort    int
	startDataDir string
	inMemory     bool
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to configuration file")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "", "Log level (debug, info, warn, error)")
	rootCmd.Flags().IntVar(&startPort, "port", 0, "Embedded NATS server port override (embedded mode only)")
	rootCmd.Flags().StringVar(&startDataDir, "store-dir", "", "Embedded NATS JetStream store directory override")
	rootCmd.Flags().BoolVar(&inMemory, "in-memory", false, "Use in-memory JetStream storage (data lost on restart)")

	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getLogLevel() slog.Level {
	level := logLevel
	if level == "" {
		level = os.Getenv("LOG_LEVEL")
	}

	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}

	return slog.LevelInfo
}

func setupLogging() {
	setupLogWriter(os.Stdout)
}

func setupLogWriter(w io.Writer) {
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: getLogLevel(),
	}))
	slog.SetDefault(logger)
}

package commands

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "grctl",
	Short: "grctl - Workflow Execution Engine",
	Long: `grctl is a lightweight workflow execution engine built on NATS.io.

It provides durable workflow execution with state management, task checkpointing,
and horizontal scalability through NATS messaging.`,
	SilenceUsage: true,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("grctl %s\n", version)
		fmt.Printf("  commit: %s\n", commit)
		fmt.Printf("  built:  %s\n", date)
	},
}

var (
	logLevel  string
	serverURL string
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVarP(&serverURL, "server-url", "s", "localhost:4225", "grctl server address (e.g. localhost:4225 or prod.example.com:4225)")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(workflowCmd)
}

// normalizeServerURL prepends nats:// if no scheme is present, allowing users
// to pass plain host:port. Explicit schemes (e.g. tls://) are left untouched.
func normalizeServerURL(url string) string {
	if !strings.Contains(url, "://") {
		return "nats://" + url
	}
	return url
}

func Execute() error {
	return rootCmd.Execute()
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

// setupLogging configures slog to write to stdout (for server commands).
func setupLogging() {
	setupLogWriter(os.Stdout)
}

// setupFileLogging configures slog to write to a file (for TUI commands
// where stdout is captured by Bubble Tea). Returns a cleanup function.
func setupFileLogging() func() {
	f, err := os.OpenFile("/tmp/grctl-tui.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return func() {}
	}
	setupLogWriter(f)
	return func() {
		err := f.Close()
		if err != nil {
			slog.Error("failed to close log file", "error", err)
		}
	}
}

func setupLogWriter(w io.Writer) {
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: getLogLevel(),
	}))
	slog.SetDefault(logger)
}

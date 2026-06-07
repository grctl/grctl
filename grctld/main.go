package main

import (
	"fmt"
	"os"

	"grctl/server/config"
	"grctl/server/grctld/srvlog"

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
	logFormat    string
	startPort    int
	startDataDir string
	inMemory     bool
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "Path to configuration file")
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVarP(&logFormat, "log-format", "", "", "Log format (json, text)")
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

func getLogLevel() string {
	level := logLevel
	if level == "" {
		level = os.Getenv("LOG_LEVEL")
	}
	if level == "" {
		level = "info"
	}
	return level
}

func getLogFormat() string {
	if logFormat != "" {
		return logFormat
	}
	if format := os.Getenv("GRCTL_LOG_FORMAT"); format != "" {
		return format
	}
	return ""
}

func initLogging() {
	srvlog.Init(srvlog.Config{
		Level:  getLogLevel(),
		Format: getLogFormat(),
	})
}

func reinitLogging(cmd *cobra.Command, cfg config.Config) {
	level := cfg.Logging.Level
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		level = v
	}
	if cmd.Flags().Changed("log-level") {
		level = logLevel
	}

	format := cfg.Logging.Format
	if v := os.Getenv("GRCTL_LOG_FORMAT"); v != "" {
		format = v
	}
	if cmd.Flags().Changed("log-format") {
		format = logFormat
	}

	srvlog.Init(srvlog.Config{
		Level:     level,
		Format:    format,
		AddSource: cfg.Logging.AddSource,
	})
}

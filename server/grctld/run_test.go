package main

import (
	"testing"

	"grctl/server/config"

	"github.com/spf13/cobra"
)

func TestApplyStartConfigOverrides_PortFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("port", 0, "")
	if err := cmd.Flags().Set("port", "4333"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	cfg := config.Config{
		NATS: config.NATSConfig{Mode: config.NATSModeEmbedded, Port: 4225},
		Streams: config.StreamsConfig{
			Storage: "file",
		},
		Defaults: config.DefaultsConfig{
			WorkerResponseTimeout: 1,
			StepTimeout:           1,
		},
	}

	startPort = 4333
	if err := applyStartConfigOverrides(cmd, &cfg); err != nil {
		t.Fatalf("apply overrides: %v", err)
	}
	if cfg.NATS.Port != 4333 {
		t.Fatalf("expected port override to be applied, got %d", cfg.NATS.Port)
	}
}

func TestApplyStartConfigOverrides_InvalidEmbeddedPort(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("port", 0, "")
	if err := cmd.Flags().Set("port", "0"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	cfg := config.Config{
		NATS: config.NATSConfig{Mode: config.NATSModeEmbedded, Port: 4225},
		Streams: config.StreamsConfig{
			Storage: "file",
		},
		Defaults: config.DefaultsConfig{
			WorkerResponseTimeout: 1,
			StepTimeout:           1,
		},
	}

	startPort = 0
	if err := applyStartConfigOverrides(cmd, &cfg); err == nil {
		t.Fatal("expected error for invalid embedded port")
	}
}

func TestApplyStartConfigOverrides_ExternalModePortNotRequired(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int("port", 0, "")
	if err := cmd.Flags().Set("port", "0"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	cfg := config.Config{
		NATS: config.NATSConfig{
			Mode: config.NATSModeExternal,
			URL:  "nats://127.0.0.1:4222",
			Port: 4225,
		},
		Streams: config.StreamsConfig{
			Storage: "file",
		},
		Defaults: config.DefaultsConfig{
			WorkerResponseTimeout: 1,
			StepTimeout:           1,
		},
	}

	startPort = 0
	if err := applyStartConfigOverrides(cmd, &cfg); err != nil {
		t.Fatalf("expected no error in external mode, got %v", err)
	}
	if cfg.NATS.Port != 0 {
		t.Fatalf("expected overridden port to be stored, got %d", cfg.NATS.Port)
	}
}

func TestApplyStartConfigOverrides_InMemoryFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Bool("in-memory", false, "")
	if err := cmd.Flags().Set("in-memory", "true"); err != nil {
		t.Fatalf("set flag: %v", err)
	}

	cfg := config.Config{
		NATS: config.NATSConfig{Mode: config.NATSModeEmbedded, Port: 4225},
		Streams: config.StreamsConfig{
			Storage: "file",
		},
		Defaults: config.DefaultsConfig{
			WorkerResponseTimeout: 1,
			StepTimeout:           1,
		},
	}

	if err := applyStartConfigOverrides(cmd, &cfg); err != nil {
		t.Fatalf("apply overrides: %v", err)
	}
	if cfg.Streams.Storage != "memory" {
		t.Fatalf("expected streams.storage to be \"memory\", got %q", cfg.Streams.Storage)
	}
}

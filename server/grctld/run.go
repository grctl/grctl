package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"grctl/server/config"
	"grctl/server/natsembd"
	"grctl/server/server"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/spf13/cobra"
)

func runServer(cmd *cobra.Command, args []string) error {
	setupLogging()

	slog.Info("grctl Server starting...", "log_level", getLogLevel().String())

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return err
	}
	if err := applyStartConfigOverrides(cmd, &cfg); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	nc, js, ns, err := startNATS(cfg)
	if err != nil {
		slog.Error("failed to initialize NATS", "error", err)
		return err
	}

	s, err := server.NewServer(ctx, nc, js, &cfg, &server.Options{InMemory: cfg.InMemoryStreams()})
	if err != nil {
		slog.Error("failed to create server instance", "error", err)
		return err
	}

	err = s.Start()
	if err != nil {
		slog.Error("failed to start server", "error", err)
		return err
	}

	slog.Info("grctl Server started successfully")

	// Wait for shutdown signal and then shut down gracefully
	<-ctx.Done()
	shutdown(s, nc, ns)
	return nil
}

func startNATS(cfg config.Config) (*nats.Conn, jetstream.JetStream, *natsserver.Server, error) {
	if cfg.NATS.Mode == config.NATSModeEmbedded {
		slog.Info("Using embedded NATS mode", "effective_port", cfg.NATS.Port)
		return natsembd.RunEmbeddedServerWithConfig(cfg.NATS.ConfigFile, cfg.NATS.Port)
	}

	nc, err := nats.Connect(cfg.NATS.URL)
	if err != nil {
		return nil, nil, nil, err
	}

	js, err := natsembd.NewJetStreamContext(nc)
	if err != nil {
		nc.Close()
		return nil, nil, nil, err
	}

	return nc, js, nil, nil
}

func applyStartConfigOverrides(cmd *cobra.Command, cfg *config.Config) error {
	if cmd.Flags().Changed("port") {
		cfg.NATS.Port = startPort
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid --port value: %w", err)
		}
	}
	if cmd.Flags().Changed("in-memory") {
		cfg.Streams.Storage = "memory"
	}
	return nil
}

func shutdown(s *server.Server, nc *nats.Conn, ns *natsserver.Server) {
	slog.Info("Shutdown signal received, stopping server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.Stop(shutdownCtx); err != nil {
		slog.Error("Forced shutdown after timeout", "error", err)
	} else {
		slog.Info("Server stopped cleanly")
	}

	nc.Close()

	if ns != nil {
		ns.Shutdown()
		ns.WaitForShutdown()
	}
}

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
	"grctl/server/natsreg"
	"grctl/server/server"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/spf13/cobra"
)

func runServer(cmd *cobra.Command, args []string) error {
	initLogging()

	if err := natsreg.Init(); err != nil {
		slog.Error("failed to initialize nats manifest", "error", err)
		return err
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return err
	}
	if err := applyStartConfigOverrides(cmd, &cfg); err != nil {
		return err
	}

	reinitLogging(cmd, cfg)

	slog.Info("grctl server starting", "log_level", cfg.Logging.Level)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)
	go func() {
		for range sighupCh {
			slog.Info("sighup received, reloading configuration")
			newCfg, err := config.Load(configPath)
			if err != nil {
				slog.Error("failed to reload config on sighup", "error", err)
				continue
			}
			if err := applyStartConfigOverrides(cmd, &newCfg); err != nil {
				slog.Error("failed to apply config overrides on sighup", "error", err)
				continue
			}
			reinitLogging(cmd, newCfg)
			slog.Info("log level reloaded on sighup")
		}
	}()

	nc, js, ns, err := startNATS(cfg)
	if err != nil {
		slog.Error("failed to initialize NATS", "error", err)
		return err
	}

	s, err := server.NewServer(ctx, nc, js, &cfg, &server.Options{InMemory: cfg.NATS.InMemory()})
	if err != nil {
		slog.Error("failed to create server instance", "error", err)
		return err
	}

	err = s.Start()
	if err != nil {
		slog.Error("failed to start server", "error", err)
		return err
	}

	slog.Info("grctl server started successfully")

	// Wait for shutdown signal and then shut down gracefully
	<-ctx.Done()
	shutdown(s, nc, ns)
	return nil
}

func startNATS(cfg config.Config) (*nats.Conn, jetstream.JetStream, *natsserver.Server, error) {
	if cfg.NATS.Mode == config.NATSModeEmbedded {
		slog.Info("using embedded NATS mode", "effective_port", cfg.NATS.Port)
		return natsembd.RunEmbeddedServerWithConfig(cfg.NATS)
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
	if cmd.Flags().Changed("store-dir") {
		cfg.NATS.StoreDir = startDataDir
	}
	if cmd.Flags().Changed("in-memory") {
		cfg.NATS.Storage = "memory"
	}
	return nil
}

func shutdown(s *server.Server, nc *nats.Conn, ns *natsserver.Server) {
	slog.Info("shutdown signal received, stopping server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.Stop(shutdownCtx); err != nil {
		slog.Error("forced shutdown after timeout", "error", err)
	} else {
		slog.Info("server stopped cleanly")
	}

	nc.Close()

	if ns != nil {
		ns.Shutdown()
		ns.WaitForShutdown()
	}
}

package natsembd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"grctl/server/config"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const embeddedServerReadyTimeout = 15 * time.Second

type natsSlogAdapter struct{}

func (natsSlogAdapter) Noticef(format string, v ...any) {
	slog.Default().With("subsystem", "nats").Info(fmt.Sprintf(format, v...))
}

func (natsSlogAdapter) Warnf(format string, v ...any) {
	slog.Default().With("subsystem", "nats").Warn(fmt.Sprintf(format, v...))
}

func (natsSlogAdapter) Errorf(format string, v ...any) {
	slog.Default().With("subsystem", "nats").Error(fmt.Sprintf(format, v...))
}

func (natsSlogAdapter) Fatalf(format string, v ...any) {
	slog.Default().With("subsystem", "nats").Error(fmt.Sprintf(format, v...))
}

func (natsSlogAdapter) Debugf(format string, v ...any) {
	slog.Default().With("subsystem", "nats").Debug(fmt.Sprintf(format, v...))
}

func (natsSlogAdapter) Tracef(format string, v ...any) {
	slog.Default().With("subsystem", "nats").Debug(fmt.Sprintf(format, v...))
}

func RunEmbeddedServerWithConfig(natsCfg config.NATSConfig) (*nats.Conn, jetstream.JetStream, *natsserver.Server, error) {
	opts, err := resolveEmbeddedOptions(natsCfg)
	if err != nil {
		return nil, nil, nil, err
	}
	return runEmbeddedServer(opts)
}

func resolveEmbeddedOptions(natsCfg config.NATSConfig) (*natsserver.Options, error) {
	opts := &natsserver.Options{
		ServerName: natsCfg.ServerName,
		Host:       "127.0.0.1",
		JetStream:  true,
		NoLog:      true,
		NoSigs:     true,
	}

	if strings.TrimSpace(natsCfg.ConfigFile) != "" {
		fileOpts, err := natsserver.ProcessConfigFile(natsCfg.ConfigFile)
		if err != nil {
			return nil, fmt.Errorf("process NATS config file: %w", err)
		}
		if !fileOpts.JetStream {
			return nil, errors.New("JetStream must be enabled in NATS config")
		}
		opts = fileOpts
	}

	// grctl.yaml and flags always win for these fields.
	opts.ServerName = natsCfg.ServerName
	opts.Port = natsCfg.Port
	opts.StoreDir = natsCfg.StoreDir
	applySyncInterval(opts, natsCfg.SyncInterval)

	return opts, nil
}

func applySyncInterval(opts *natsserver.Options, syncInterval string) {
	if syncInterval == "always" {
		opts.SyncAlways = true
		opts.SyncInterval = 0
		return
	}
	if d, err := time.ParseDuration(syncInterval); err == nil {
		opts.SyncAlways = false
		opts.SyncInterval = d
	}
}

func NewJetStreamContext(nc *nats.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}

	maxRetries := 10
	retryDelay := 500 * time.Millisecond
	for i := range maxRetries {
		_, err = js.AccountInfo(context.Background())
		if err == nil {
			return js, nil
		}
		if i == maxRetries-1 {
			return nil, fmt.Errorf("jetstream not ready after %d retries: %w", maxRetries, err)
		}
		time.Sleep(retryDelay)
	}

	return js, nil
}

func runEmbeddedServer(opts *natsserver.Options) (*nats.Conn, jetstream.JetStream, *natsserver.Server, error) {
	slog.Info("embedded nats starting", "port", opts.Port, "server_name", opts.ServerName)
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		return nil, nil, nil, err
	}

	ns.SetLogger(natsSlogAdapter{}, false, false)

	go ns.Start()

	if !ns.ReadyForConnections(embeddedServerReadyTimeout) {
		running := ns.Running()
		clientURL := ns.ClientURL()
		ns.Shutdown()
		return nil, nil, nil, fmt.Errorf("NATS server timeout after %s (running=%t client_url=%q)", embeddedServerReadyTimeout, running, clientURL)
	}

	clientOpts := []nats.Option{nats.InProcessServer(ns)}
	nc, err := nats.Connect(ns.ClientURL(), clientOpts...)
	if err != nil {
		ns.Shutdown()
		return nil, nil, nil, err
	}

	js, err := NewJetStreamContext(nc)
	if err != nil {
		nc.Close()
		ns.Shutdown()
		return nil, nil, nil, err
	}

	return nc, js, ns, nil
}

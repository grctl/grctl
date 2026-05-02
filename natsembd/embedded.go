package natsembd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"grctl/server/config"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const embeddedServerReadyTimeout = 15 * time.Second

func RunEmbeddedServerWithConfig(natsCfg config.NATSConfig) (*nats.Conn, jetstream.JetStream, *server.Server, error) {
	opts, err := resolveEmbeddedOptions(natsCfg)
	if err != nil {
		return nil, nil, nil, err
	}
	return runEmbeddedServer(opts)
}

func resolveEmbeddedOptions(natsCfg config.NATSConfig) (*server.Options, error) {
	opts := &server.Options{
		ServerName: natsCfg.ServerName,
		Host:       "127.0.0.1",
		JetStream:  true,
		NoLog:      true,
		NoSigs:     true,
	}

	if strings.TrimSpace(natsCfg.ConfigFile) != "" {
		fileOpts, err := server.ProcessConfigFile(natsCfg.ConfigFile)
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

func applySyncInterval(opts *server.Options, syncInterval string) {
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

func runEmbeddedServer(opts *server.Options) (*nats.Conn, jetstream.JetStream, *server.Server, error) {
	slog.Info("Embedded NATS starting", "port", opts.Port, "server_name", opts.ServerName)
	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, nil, nil, err
	}

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

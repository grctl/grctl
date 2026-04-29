package natsembd

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grctl/server/config"
)

func TestNatsConfPortIgnored(t *testing.T) {
	effectivePort := getFreePort(t)
	configPort := getFreePort(t)
	storeDir := t.TempDir()

	configData := fmt.Appendf(nil, "server_name: \"grctl-test\"\nport: %d\njetstream {\n  store_dir: %q\n}", configPort, storeDir)
	configPath := filepath.Join(t.TempDir(), "nats.conf")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	natsCfg := config.NATSConfig{
		ConfigFile:   configPath,
		Port:         effectivePort,
		StoreDir:     storeDir,
		SyncInterval: "always",
	}
	nc, js, ns, err := RunEmbeddedServerWithConfig(natsCfg)
	if err != nil {
		t.Fatalf("run embedded server: %v", err)
	}
	t.Cleanup(func() {
		nc.Close()
		ns.Shutdown()
		ns.WaitForShutdown()
	})

	if js == nil {
		t.Fatal("expected jetstream context")
	}

	addr, ok := ns.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected TCP address, got %T", ns.Addr())
	}
	if addr.Port != effectivePort {
		t.Fatalf("expected effective port %d, got %d", effectivePort, addr.Port)
	}
}

func TestNatsConfStoreDir(t *testing.T) {
	grctlStoreDir := t.TempDir()
	confStoreDir := t.TempDir()

	// nats.conf declares a store_dir, but NATSConfig.StoreDir should win.
	configData := fmt.Appendf(nil, "jetstream {\n  store_dir: %q\n}", confStoreDir)
	configPath := filepath.Join(t.TempDir(), "nats.conf")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	natsCfg := config.NATSConfig{
		ConfigFile:   configPath,
		Port:         4225,
		StoreDir:     grctlStoreDir,
		SyncInterval: "always",
	}
	opts, err := resolveEmbeddedOptions(natsCfg)
	if err != nil {
		t.Fatalf("resolve options: %v", err)
	}
	if opts.StoreDir != grctlStoreDir {
		t.Fatalf("expected grctl store_dir %q, got %q", grctlStoreDir, opts.StoreDir)
	}
}

func TestResolveEmbeddedOptions_NoConfigFileUsesNATSConfig(t *testing.T) {
	natsCfg := config.NATSConfig{
		Port:         4225,
		StoreDir:     "/tmp/grctl-test",
		SyncInterval: "always",
	}
	opts, err := resolveEmbeddedOptions(natsCfg)
	if err != nil {
		t.Fatalf("resolve options: %v", err)
	}
	if !opts.JetStream {
		t.Fatal("expected JetStream to be enabled by default")
	}
	if opts.StoreDir != "/tmp/grctl-test" {
		t.Fatalf("expected store dir /tmp/grctl-test, got %q", opts.StoreDir)
	}
	if opts.Host != "127.0.0.1" {
		t.Fatalf("expected default host 127.0.0.1, got %q", opts.Host)
	}
	if !opts.SyncAlways {
		t.Fatal("expected SyncAlways to be true for sync_interval=always")
	}
}

func TestResolveEmbeddedOptions_RequiresJetStreamFromConfig(t *testing.T) {
	configData := []byte(`port: 4222`)
	configPath := filepath.Join(t.TempDir(), "nats.conf")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	natsCfg := config.NATSConfig{
		ConfigFile:   configPath,
		Port:         4225,
		StoreDir:     t.TempDir(),
		SyncInterval: "always",
	}
	_, err := resolveEmbeddedOptions(natsCfg)
	if err == nil {
		t.Fatal("expected error when JetStream is disabled")
	}
	if !strings.Contains(err.Error(), "JetStream must be enabled") {
		t.Fatalf("expected jetstream error, got %v", err)
	}
}

func getFreePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("get free port: %v", err)
	}
	defer func() { _ = l.Close() }()

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("expected TCP addr, got %T", l.Addr())
	}
	return addr.Port
}

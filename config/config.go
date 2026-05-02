package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

const (
	NATSModeEmbedded = "embedded"
	NATSModeExternal = "external"
)

type Config struct {
	NATS     NATSConfig     `koanf:"nats"`
	Defaults DefaultsConfig `koanf:"defaults"`
}

type NATSConfig struct {
	ServerName   string `koanf:"server_name"`
	Mode         string `koanf:"mode"`
	URL          string `koanf:"url"`
	ConfigFile   string `koanf:"config_file"`
	Port         int    `koanf:"port"`
	StoreDir     string `koanf:"store_dir"`
	SyncInterval string `koanf:"sync_interval"`
	Storage      string `koanf:"storage"`
}

func (n NATSConfig) InMemory() bool {
	return n.Storage == "memory"
}

func (n NATSConfig) ResolveStoreDir() NATSConfig {
	if !strings.HasPrefix(n.StoreDir, "~/") {
		return n
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("could not resolve home directory for store_dir, using fallback", "error", err, "fallback", "./data")
		n.StoreDir = "./data"
		return n
	}
	n.StoreDir = filepath.Join(home, n.StoreDir[2:])
	return n
}

type DefaultsConfig struct {
	WorkerResponseTimeout time.Duration `koanf:"worker_response_timeout"`
	StepTimeout           time.Duration `koanf:"step_timeout"`
}

func Load(path string) (Config, error) {
	k := koanf.New(".")
	err := k.Load(confmap.Provider(defaultConfigMap(), "."), nil)
	if err != nil {
		return Config{}, fmt.Errorf("load default config: %w", err)
	}

	if path != "" {
		err = loadConfigFile(k, path)
		if err != nil {
			return Config{}, err
		}
	}

	err = k.Load(env.Provider("GRCTL_", ".", envKeyMapper), nil)
	if err != nil {
		return Config{}, fmt.Errorf("load env config: %w", err)
	}

	cfg, err := unmarshalConfig(k)
	if err != nil {
		return Config{}, err
	}

	cfg = cfg.Normalized()
	cfg.NATS = cfg.NATS.ResolveStoreDir()
	err = cfg.Validate()
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Normalized() Config {
	cfg := c
	cfg.NATS.Mode = strings.ToLower(strings.TrimSpace(cfg.NATS.Mode))
	cfg.NATS.Storage = strings.ToLower(strings.TrimSpace(cfg.NATS.Storage))
	return cfg
}

func (c Config) Validate() error {
	switch c.NATS.Mode {
	case NATSModeEmbedded:
		if c.NATS.Port < 1 || c.NATS.Port > 65535 {
			return fmt.Errorf("port must be in range 1..65535 for embedded mode")
		}
		if c.NATS.StoreDir == "" {
			return fmt.Errorf("nats.store_dir must be non-empty in embedded mode")
		}
	case NATSModeExternal:
		if c.NATS.URL == "" {
			return fmt.Errorf("nats.url is required for external mode")
		}
	default:
		return fmt.Errorf("nats.mode must be %q or %q", NATSModeEmbedded, NATSModeExternal)
	}

	if err := validateSyncInterval(c.NATS.SyncInterval); err != nil {
		return err
	}

	switch c.NATS.Storage {
	case "memory", "file":
	default:
		return fmt.Errorf("nats.storage must be \"memory\" or \"file\"")
	}

	if c.Defaults.WorkerResponseTimeout <= 0 {
		return fmt.Errorf("defaults.worker_ack_timeout must be positive")
	}
	if c.Defaults.StepTimeout <= 0 {
		return fmt.Errorf("defaults.step_timeout must be positive")
	}

	return nil
}

func validateSyncInterval(s string) error {
	if s == "always" {
		return nil
	}
	_, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("nats.sync_interval must be \"always\" or a valid duration (e.g. \"1s\"), got %q", s)
	}
	return nil
}

func loadConfigFile(k *koanf.Koanf, path string) error {
	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat config file %q: %w", path, err)
	}

	err = k.Load(file.Provider(path), yaml.Parser())
	if err != nil {
		return fmt.Errorf("load config file %q: %w", path, err)
	}

	return nil
}

func envKeyMapper(key string) string {
	key = strings.ToLower(key)
	key = strings.TrimPrefix(key, "grctl_")
	firstUnderscore := strings.Index(key, "_")
	if firstUnderscore == -1 {
		return key
	}
	return key[:firstUnderscore] + "." + key[firstUnderscore+1:]
}

func unmarshalConfig(k *koanf.Koanf) (Config, error) {
	cfg := Config{}
	decoderConfig := &mapstructure.DecoderConfig{
		Result:           &cfg,
		TagName:          "koanf",
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
		),
	}

	err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{DecoderConfig: decoderConfig})
	if err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	return cfg, nil
}

func LoadConfig() (*Config, error) {
	configPath := flag.String("config", "config/grctl.yaml", "Path to grctl config file")
	cfg, err := Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return nil, err
	}
	return &cfg, nil
}

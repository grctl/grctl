package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ConfigSuite struct {
	suite.Suite
	home string
}

func (s *ConfigSuite) SetupSuite() {
	home, err := os.UserHomeDir()
	s.Require().NoError(err, "get home dir")
	s.home = home
}

func TestConfigSuite(t *testing.T) {
	suite.Run(t, new(ConfigSuite))
}

func (s *ConfigSuite) TestLoadConfigFileNotFound() {
	cfg, err := Load("/nonexistent/path/grctl.yaml")
	s.Require().NoError(err)

	s.Equal(NATSModeEmbedded, cfg.NATS.Mode)
	s.Equal("grctl", cfg.NATS.ServerName)
	s.Equal("", cfg.NATS.ConfigFile)
	s.Equal(4225, cfg.NATS.Port)
	s.Equal("file", cfg.NATS.Storage)
	s.Equal(filepath.Join(s.home, ".grctl/data"), cfg.NATS.StoreDir)
	s.Equal("always", cfg.NATS.SyncInterval)
	s.Equal(5*time.Second, cfg.Defaults.WorkerResponseTimeout)
	s.Equal(5*time.Minute, cfg.Defaults.StepTimeout)
}

func (s *ConfigSuite) TestLoadDefaults() {
	cfg, err := Load("/nonexistent/path/grctl.yaml")
	s.Require().NoError(err)

	s.Equal(NATSModeEmbedded, cfg.NATS.Mode)
	s.Equal(4225, cfg.NATS.Port)
	s.Equal("file", cfg.NATS.Storage)
	s.Equal(filepath.Join(s.home, ".grctl/data"), cfg.NATS.StoreDir)
	s.Equal("always", cfg.NATS.SyncInterval)
	s.Equal(5*time.Second, cfg.Defaults.WorkerResponseTimeout)
	s.Equal(5*time.Minute, cfg.Defaults.StepTimeout)
}

func (s *ConfigSuite) TestStoreDirExpansion() {
	s.Run("tilde expanded to absolute path", func() {
		cfg, err := Load("/nonexistent/path/grctl.yaml")
		s.Require().NoError(err)
		s.Equal(filepath.Join(s.home, ".grctl/data"), cfg.NATS.StoreDir)
		s.NotEqual('~', cfg.NATS.StoreDir[0])
	})

	s.Run("absolute path left unchanged", func() {
		configData := []byte("nats:\n  store_dir: /tmp/grctl-data\n  mode: embedded\n  port: 4225\n  storage: file\n")
		tmpDir := s.T().TempDir()
		configPath := filepath.Join(tmpDir, "grctl.yaml")
		s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

		cfg, err := Load(configPath)
		s.Require().NoError(err)
		s.Equal("/tmp/grctl-data", cfg.NATS.StoreDir)
	})
}

func (s *ConfigSuite) TestLoadConfigFile() {
	configData := []byte(`nats:
  mode: external
  url: "nats://127.0.0.1:4222"
  storage: memory
defaults:
  worker_response_timeout: "45s"
  step_timeout: "2m"
`)
	tmpDir := s.T().TempDir()
	configPath := filepath.Join(tmpDir, "grctl.yaml")
	s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

	cfg, err := Load(configPath)
	s.Require().NoError(err)

	s.Equal(NATSModeExternal, cfg.NATS.Mode)
	s.Equal("nats://127.0.0.1:4222", cfg.NATS.URL)
	s.Equal("memory", cfg.NATS.Storage)
	s.Equal(45*time.Second, cfg.Defaults.WorkerResponseTimeout)
	s.Equal(2*time.Minute, cfg.Defaults.StepTimeout)
}

func (s *ConfigSuite) TestPortFromYAML() {
	configData := []byte("nats:\n  mode: embedded\n  port: 9999\n  storage: file\n")
	tmpDir := s.T().TempDir()
	configPath := filepath.Join(tmpDir, "grctl.yaml")
	s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

	cfg, err := Load(configPath)
	s.Require().NoError(err)
	s.Equal(9999, cfg.NATS.Port)
}

func (s *ConfigSuite) TestLoadConfigEnvOverrides() {
	s.T().Setenv("GRCTL_NATS_PORT", "4333")
	s.T().Setenv("GRCTL_NATS_CONFIG_FILE", "/tmp/custom-nats.conf")
	s.T().Setenv("GRCTL_DEFAULTS_WORKER_RESPONSE_TIMEOUT", "7s")

	cfg, err := Load("/nonexistent/path/grctl.yaml")
	s.Require().NoError(err)

	s.Equal(4333, cfg.NATS.Port)
	s.Equal("/tmp/custom-nats.conf", cfg.NATS.ConfigFile)
	s.Equal(7*time.Second, cfg.Defaults.WorkerResponseTimeout)
}

func (s *ConfigSuite) TestLoadConfigEmbeddedModeInvalidPort() {
	configData := []byte("nats:\n  mode: embedded\n  port: 0\n")
	tmpDir := s.T().TempDir()
	configPath := filepath.Join(tmpDir, "grctl.yaml")
	s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

	_, err := Load(configPath)
	s.Error(err)
}

func (s *ConfigSuite) TestLoadConfigExternalModeIgnoresPort() {
	configData := []byte("nats:\n  mode: external\n  url: \"nats://127.0.0.1:4222\"\n  port: 0\n")
	tmpDir := s.T().TempDir()
	configPath := filepath.Join(tmpDir, "grctl.yaml")
	s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

	cfg, err := Load(configPath)
	s.Require().NoError(err)
	s.Equal(0, cfg.NATS.Port)
}

func (s *ConfigSuite) TestValidationEmbeddedEmptyStoreDir() {
	configData := []byte("nats:\n  mode: embedded\n  port: 4225\n  storage: file\n  store_dir: \"\"\n")
	tmpDir := s.T().TempDir()
	configPath := filepath.Join(tmpDir, "grctl.yaml")
	s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

	_, err := Load(configPath)
	s.Error(err)
	s.Contains(err.Error(), "store_dir")
}

func (s *ConfigSuite) TestValidationInvalidSyncInterval() {
	configData := []byte("nats:\n  mode: embedded\n  port: 4225\n  storage: file\n  sync_interval: \"banana\"\n")
	tmpDir := s.T().TempDir()
	configPath := filepath.Join(tmpDir, "grctl.yaml")
	s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

	_, err := Load(configPath)
	s.Error(err)
	s.Contains(err.Error(), "sync_interval")
}

func (s *ConfigSuite) TestValidationValidDurationSyncInterval() {
	configData := []byte("nats:\n  mode: embedded\n  port: 4225\n  storage: file\n  sync_interval: \"500ms\"\n")
	tmpDir := s.T().TempDir()
	configPath := filepath.Join(tmpDir, "grctl.yaml")
	s.Require().NoError(os.WriteFile(configPath, configData, 0o600))

	cfg, err := Load(configPath)
	s.Require().NoError(err)
	s.Equal("500ms", cfg.NATS.SyncInterval)
}

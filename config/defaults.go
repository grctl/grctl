package config

func defaultConfigMap() map[string]any {
	return map[string]any{
		"nats.server_name":      "grctl",
		"nats.mode":             NATSModeEmbedded,
		"nats.port":             4225,
		"nats.store_dir":        "~/.grctl/data",
		"nats.sync_interval":    "always",
		"nats.storage":          "file",
		"defaults.step_timeout": "5m",
		"defaults.wait_timeout": "1h",
		"logging.level":         "info",
		"logging.format":        "",
		"logging.add_source":    false,
	}
}

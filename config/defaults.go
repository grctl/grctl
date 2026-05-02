package config

func defaultConfigMap() map[string]any {
	return map[string]any{
		"nats.server_name":                 "grctl",
		"nats.mode":                        NATSModeEmbedded,
		"nats.port":                        4225,
		"nats.store_dir":                   "~/.grctl/data",
		"nats.sync_interval":               "always",
		"nats.storage":                     "file",
		"defaults.worker_response_timeout": "5s",
		"defaults.step_timeout":            "5m",
	}
}

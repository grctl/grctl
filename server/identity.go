package server

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"grctl/server/config"
)

func deriveServerID(cfg *config.Config) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("get hostname: %w", err)
	}

	var port string
	switch cfg.NATS.Mode {
	case config.NATSModeEmbedded:
		port = strconv.Itoa(cfg.NATS.Port)
	case config.NATSModeExternal:
		u, err := url.Parse(cfg.NATS.URL)
		if err != nil {
			return "", fmt.Errorf("parse NATS URL: %w", err)
		}
		port = u.Port()
	}

	return "s:" + hostname + ":" + port, nil
}

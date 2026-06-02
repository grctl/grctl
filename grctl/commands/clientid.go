package commands

import (
	"fmt"
	"os"
)

func deriveClientID() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("get hostname: %w", err)
	}
	return "c:" + hostname, nil
}

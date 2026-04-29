// Package testutil provides shared test helpers for server packages.
package testutil

import (
	"grctl/server/config"
	"grctl/server/natsembd"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// RunEmbeddedNATS starts an embedded NATS server on a random port with the
// given store directory.
func RunEmbeddedNATS(storeDir string) (*nats.Conn, jetstream.JetStream, *natsserver.Server, error) {
	natsCfg := config.NATSConfig{
		Port:         natsserver.RANDOM_PORT,
		StoreDir:     storeDir,
		SyncInterval: "always",
	}
	return natsembd.RunEmbeddedServerWithConfig(natsCfg)
}

package server

import (
	"errors"
	"fmt"
	"time"

	"grctl/server/natsreg"
	model "grctl/server/types"
	external "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

// ErrWorkerUnreachable is returned when a command cannot be delivered to the
// target worker — either no active subscription exists or the request timed out.
// Callers that need retry can use errors.Is to distinguish this from other errors.
var ErrWorkerUnreachable = model.ErrWorkerUnreachable

const workerCmdTimeout = 5 * time.Second

// PublishWorkerCommand sends cmd to the named worker over a core-NATS request-reply
// channel and waits for an ACK. It stamps cmd.SenderID with the server's identity.
//
// Returns ErrWorkerUnreachable if no worker is subscribed or the request times out.
// Returns nil on ACK. No retry is performed — the caller is responsible.
func (s *Server) PublishWorkerCommand(workerID string, cmd external.Command) error {
	cmd.SenderID = s.senderID

	data, err := msgpack.Marshal(&cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal worker command: %w", err)
	}

	subject := natsreg.Manifest.WorkerCmdSubject(workerID)
	_, err = s.nc.Request(subject, data, workerCmdTimeout)
	if err != nil {
		if errors.Is(err, nats.ErrNoResponders) || errors.Is(err, nats.ErrTimeout) {
			return model.ErrWorkerUnreachable
		}
		return fmt.Errorf("worker command request failed: %w", err)
	}

	return nil
}

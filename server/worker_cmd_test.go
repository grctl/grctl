//go:build integration

package server_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"grctl/server/config"
	"grctl/server/natsreg"
	"grctl/server/server"
	"grctl/server/testutil"
	external "grctl/server/types/external/v1"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T) (*server.Server, *nats.Conn) {
	t.Helper()
	nc, js, ns, err := testutil.RunEmbeddedNATS(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		nc.Close()
		ns.Shutdown()
	})

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			WorkerResponseTimeout: 5 * time.Second,
			StepTimeout:           5 * time.Minute,
		},
	}
	srv, err := server.NewServer(context.Background(), nc, js, cfg, &server.Options{InMemory: true})
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(ctx)
	})
	return srv, nc
}

func makeTestCommand(t *testing.T) external.Command {
	t.Helper()
	return external.Command{
		ID:        external.NewCmdID(),
		Kind:      external.CmdKindRunCancel,
		Timestamp: time.Now().UTC(),
		// SenderID will be stamped by PublishWorkerCommand
		Msg: &external.CancelCmd{WFID: external.WFID("wf-test"), Reason: "test"},
	}
}

func TestPublishWorkerCommand_NoSubscriber_ReturnsUnreachable(t *testing.T) {
	srv, _ := newTestServer(t)

	err := srv.PublishWorkerCommand("no-such-worker", makeTestCommand(t))

	require.Error(t, err)
	require.True(t, errors.Is(err, server.ErrWorkerUnreachable),
		"expected ErrWorkerUnreachable, got: %v", err)
}

func TestPublishWorkerCommand_LiveSubscriber_ReturnsNil(t *testing.T) {
	srv, nc := newTestServer(t)

	workerID := "worker-" + string(external.NewCmdID())
	subject := natsreg.Manifest.WorkerCmdSubject(workerID)

	// Stub subscriber that ACKs every message immediately.
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		_ = msg.Respond([]byte("ok"))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	err = srv.PublishWorkerCommand(workerID, makeTestCommand(t))
	require.NoError(t, err)
}

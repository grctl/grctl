//go:build integration

package ingress

import (
	"context"
	"grctl/server/natsreg"
	"grctl/server/store"
	"grctl/server/testutil"
	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/suite"
	"github.com/vmihailenco/msgpack/v5"
)

type BgTaskQueueTestSuite struct {
	suite.Suite
	nc     *nats.Conn
	ns     *server.Server
	js     jetstream.JetStream
	stream jetstream.Stream
}

func (s *BgTaskQueueTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns, s.js = nc, ns, js

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)
	s.stream = stream
}

func (s *BgTaskQueueTestSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestBgTaskQueue(t *testing.T) {
	suite.Run(t, new(BgTaskQueueTestSuite))
}

func (s *BgTaskQueueTestSuite) makeTask() ext.BackgroundTask {
	parentID := ext.NewDirectiveID()
	task, err := ext.NewDeleteInboxEventTask(parentID, 42)
	s.Require().NoError(err)
	return task
}

func (s *BgTaskQueueTestSuite) publishTask(t ext.BackgroundTask) {
	data, err := msgpack.Marshal(&t)
	s.Require().NoError(err)

	_, err = s.js.Publish(context.Background(), natsreg.Manifest.BgTaskSubject(), data)
	s.Require().NoError(err)
}

func (s *BgTaskQueueTestSuite) startQueue(handler BgTaskHandler) *bgTaskQueue {
	q, err := NewBgTaskQueue(context.Background(), s.stream)
	s.Require().NoError(err)
	s.Require().NoError(q.Start(handler))
	return q
}

func waitForTask(s testFailer, ch <-chan ext.BackgroundTask) ext.BackgroundTask {
	select {
	case v := <-ch:
		return v
	case <-time.After(callTimeout):
		s.Fail("timed out waiting for handler call")
		return ext.BackgroundTask{}
	}
}

// ActionProcessed → Ack(): message is handled once and not redelivered.
func (s *BgTaskQueueTestSuite) TestProcessed_HandlerCalledOnce() {
	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ ext.BackgroundTask, _ uint64) intr.HandleResult {
		called <- struct{}{}
		return intr.Processed()
	}

	s.publishTask(s.makeTask())
	q := s.startQueue(handler)
	defer q.Stop()

	waitForCall(s, called)
	assertNoCall(s, called)
}

// ActionRetryable → NakWithDelay(): message is redelivered with NumDelivered incremented.
func (s *BgTaskQueueTestSuite) TestRetryable_MessageRedelivered() {
	deliveries := make(chan uint64, 10)
	var callCount atomic.Int32
	handler := func(_ context.Context, _ ext.BackgroundTask, numDelivered uint64) intr.HandleResult {
		count := callCount.Add(1)
		deliveries <- numDelivered
		if count == 1 {
			return intr.Retryable(10 * time.Millisecond)
		}
		return intr.Processed()
	}

	s.publishTask(s.makeTask())
	q := s.startQueue(handler)
	defer q.Stop()

	first := waitForDelivery(s, deliveries)
	s.Equal(uint64(1), first)

	second := waitForDelivery(s, deliveries)
	s.Equal(uint64(2), second)

	select {
	case <-deliveries:
		s.Fail("unexpected third handler call")
	case <-time.After(noCallWindow):
	}
}

// Malformed msgpack → Ack() without calling the handler, preventing infinite retry.
func (s *BgTaskQueueTestSuite) TestMalformed_HandlerNeverCalled() {
	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ ext.BackgroundTask, _ uint64) intr.HandleResult {
		called <- struct{}{}
		return intr.Processed()
	}

	_, err := s.js.Publish(context.Background(), natsreg.Manifest.BgTaskSubject(), []byte{0xFF, 0xFE, 0x00, 0x01})
	s.Require().NoError(err)

	q := s.startQueue(handler)
	defer q.Stop()

	assertNoCall(s, called)
}

// The handler receives the exact task that was published.
func (s *BgTaskQueueTestSuite) TestCorrectTask_PassedToHandler() {
	received := make(chan ext.BackgroundTask, 1)
	handler := func(_ context.Context, t ext.BackgroundTask, _ uint64) intr.HandleResult {
		received <- t
		return intr.Processed()
	}

	expected := s.makeTask()
	s.publishTask(expected)
	q := s.startQueue(handler)
	defer q.Stop()

	got := waitForTask(s, received)
	s.Equal(expected.Kind, got.Kind)
	s.Equal(expected.DeduplicationID, got.DeduplicationID)
	s.Equal(expected.Payload, got.Payload)
}

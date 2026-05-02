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

const callTimeout = 3 * time.Second
const noCallWindow = 300 * time.Millisecond

type DirectiveQueueTestSuite struct {
	suite.Suite
	nc     *nats.Conn
	ns     *server.Server
	js     jetstream.JetStream
	stream jetstream.Stream
	wfType ext.WFType
	wfID   ext.WFID
	runID  ext.RunID
}

func (s *DirectiveQueueTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns, s.js = nc, ns, js

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)
	s.stream = stream

	s.wfType = ext.NewWFType("test")
	s.wfID = ext.NewWFID()
	s.runID = ext.NewRunID()
}

func (s *DirectiveQueueTestSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestDirectiveQueue(t *testing.T) {
	suite.Run(t, new(DirectiveQueueTestSuite))
}

func (s *DirectiveQueueTestSuite) makeDirective() ext.Directive {
	return ext.Directive{
		ID:   ext.NewDirectiveID(),
		Kind: ext.DirectiveKindEvent,
		RunInfo: ext.RunInfo{
			WFID:   s.wfID,
			ID:     s.runID,
			WFType: s.wfType,
		},
		Msg: &ext.Event{
			EventName: "test-event",
			Timeout:   1000,
		},
	}
}

func (s *DirectiveQueueTestSuite) publishDirective(d ext.Directive) {
	data, err := msgpack.Marshal(&d)
	s.Require().NoError(err)

	subject := natsreg.Manifest.DirectiveSubject(s.wfType, s.wfID, s.runID)
	_, err = s.js.Publish(context.Background(), subject, data)
	s.Require().NoError(err)
}

func (s *DirectiveQueueTestSuite) startQueue(handler DirectiveHandler) *directiveQueue {
	q, err := NewDirectiveQueue(context.Background(), s.js, s.stream)
	s.Require().NoError(err)
	s.Require().NoError(q.Start(handler))
	return q
}

type testFailer interface {
	Fail(failureMessage string, msgAndArgs ...interface{}) bool
}

func waitForCall(s testFailer, ch <-chan struct{}) {
	select {
	case <-ch:
	case <-time.After(callTimeout):
		s.Fail("timed out waiting for handler call")
	}
}

func assertNoCall(s testFailer, ch <-chan struct{}) {
	select {
	case <-ch:
		s.Fail("unexpected handler call")
	case <-time.After(noCallWindow):
	}
}

func waitForDelivery(s testFailer, ch <-chan uint64) uint64 {
	select {
	case v := <-ch:
		return v
	case <-time.After(callTimeout):
		s.Fail("timed out waiting for handler call")
		return 0
	}
}

func waitForDirective(s testFailer, ch <-chan ext.Directive) ext.Directive {
	select {
	case v := <-ch:
		return v
	case <-time.After(callTimeout):
		s.Fail("timed out waiting for handler call")
		return ext.Directive{}
	}
}

// Stop blocks until an in-flight handler completes, then returns.
func (s *DirectiveQueueTestSuite) TestStop_WaitsForInflightHandler() {
	handlerStarted := make(chan struct{})
	handlerUnblock := make(chan struct{})

	handler := func(_ context.Context, _ ext.Directive, _ uint64) intr.HandleResult {
		close(handlerStarted)
		<-handlerUnblock
		return intr.Processed()
	}

	s.publishDirective(s.makeDirective())
	q := s.startQueue(handler)

	// Wait for handler to be invoked and blocking
	waitForCall(s, handlerStarted)

	// Stop in a goroutine — it should block while handler is in-flight
	stopDone := make(chan struct{})
	go func() {
		q.Stop()
		close(stopDone)
	}()

	// Verify Stop hasn't returned yet
	select {
	case <-stopDone:
		s.Fail("Stop returned while handler was still in-flight")
	case <-time.After(200 * time.Millisecond):
	}

	// Unblock the handler
	close(handlerUnblock)

	// Stop should return promptly
	select {
	case <-stopDone:
	case <-time.After(callTimeout):
		s.Fail("Stop did not return after handler completed")
	}
}

// Stop returns immediately when no messages are in-flight.
func (s *DirectiveQueueTestSuite) TestStop_ReturnsImmediatelyWhenIdle() {
	handler := func(_ context.Context, _ ext.Directive, _ uint64) intr.HandleResult {
		return intr.Processed()
	}

	q := s.startQueue(handler)

	stopDone := make(chan struct{})
	go func() {
		q.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(callTimeout):
		s.Fail("Stop did not return promptly when idle")
	}
}

// NewDirectiveQueue with a cancelled context returns an error.
func (s *DirectiveQueueTestSuite) TestNewQueue_CancelledContext_ReturnsError() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewDirectiveQueue(ctx, s.js, s.stream)
	s.Error(err)
}

// ActionProcessed → Ack(): message is handled once and not redelivered.
func (s *DirectiveQueueTestSuite) TestProcessed_HandlerCalledOnce() {
	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ ext.Directive, _ uint64) intr.HandleResult {
		called <- struct{}{}
		return intr.Processed()
	}

	s.publishDirective(s.makeDirective())
	q := s.startQueue(handler)
	defer q.Stop()

	waitForCall(s, called)
	assertNoCall(s, called)
}

// ActionRetryable → NakWithDelay(): message is redelivered with NumDelivered incremented.
func (s *DirectiveQueueTestSuite) TestRetryable_MessageRedelivered() {
	deliveries := make(chan uint64, 10)
	var callCount atomic.Int32
	handler := func(_ context.Context, _ ext.Directive, numDelivered uint64) intr.HandleResult {
		count := callCount.Add(1)
		deliveries <- numDelivered
		if count == 1 {
			return intr.Retryable(10 * time.Millisecond)
		}
		return intr.Processed()
	}

	s.publishDirective(s.makeDirective())
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
func (s *DirectiveQueueTestSuite) TestMalformed_HandlerNeverCalled() {
	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ ext.Directive, _ uint64) intr.HandleResult {
		called <- struct{}{}
		return intr.Processed()
	}

	subject := natsreg.Manifest.DirectiveSubject(s.wfType, s.wfID, s.runID)
	_, err := s.js.Publish(context.Background(), subject, []byte{0xFF, 0xFE, 0x00, 0x01})
	s.Require().NoError(err)

	q := s.startQueue(handler)
	defer q.Stop()

	assertNoCall(s, called)
}

// The handler receives the exact directive that was published.
func (s *DirectiveQueueTestSuite) TestCorrectDirective_PassedToHandler() {
	received := make(chan ext.Directive, 1)
	handler := func(_ context.Context, d ext.Directive, _ uint64) intr.HandleResult {
		received <- d
		return intr.Processed()
	}

	expected := s.makeDirective()
	s.publishDirective(expected)
	q := s.startQueue(handler)
	defer q.Stop()

	got := waitForDirective(s, received)
	s.Equal(expected.ID, got.ID)
	s.Equal(expected.Kind, got.Kind)
	s.Equal(expected.RunInfo.WFID, got.RunInfo.WFID)
	s.Equal(expected.RunInfo.ID, got.RunInfo.ID)
	s.Equal(expected.RunInfo.WFType, got.RunInfo.WFType)
}

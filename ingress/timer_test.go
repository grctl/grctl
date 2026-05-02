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

type TimerStreamTestSuite struct {
	suite.Suite
	nc     *nats.Conn
	ns     *server.Server
	js     jetstream.JetStream
	stream jetstream.Stream
	wfID   ext.WFID
}

func (s *TimerStreamTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns, s.js = nc, ns, js

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)
	s.stream = stream

	s.wfID = ext.NewWFID()
}

func (s *TimerStreamTestSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestTimerStream(t *testing.T) {
	suite.Run(t, new(TimerStreamTestSuite))
}

func (s *TimerStreamTestSuite) makeTimer(kind ext.TimerKind) ext.Timer {
	return ext.Timer{
		ID:        ext.NewTimerID(),
		Kind:      kind,
		WFID:      s.wfID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		Directive: ext.Directive{
			ID:   ext.NewDirectiveID(),
			Kind: ext.DirectiveKindSleep,
			Msg:  &ext.Sleep{Duration: 1000, NextStepName: "next"},
		},
	}
}

func (s *TimerStreamTestSuite) publishFiredTimer(t ext.Timer) {
	data, err := msgpack.Marshal(&t)
	s.Require().NoError(err)

	_, err = s.js.Publish(context.Background(), natsreg.Manifest.TimerFiredSubject(), data)
	s.Require().NoError(err)
}

func (s *TimerStreamTestSuite) startStream(handler TimerHandler) *timerStream {
	ts, err := NewTimerStream(context.Background(), s.js, s.stream)
	s.Require().NoError(err)
	s.Require().NoError(ts.Start(handler))
	return ts
}

func waitForTimer(s testFailer, ch <-chan ext.Timer) ext.Timer {
	select {
	case v := <-ch:
		return v
	case <-time.After(callTimeout):
		s.Fail("timed out waiting for handler call")
		return ext.Timer{}
	}
}

// ActionProcessed → Ack(): message is handled once and not redelivered.
func (s *TimerStreamTestSuite) TestProcessed_HandlerCalledOnce() {
	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ ext.Timer, _ uint64) intr.HandleResult {
		called <- struct{}{}
		return intr.Processed()
	}

	s.publishFiredTimer(s.makeTimer(ext.TimerKindSleep))
	ts := s.startStream(handler)
	defer ts.Stop()

	waitForCall(s, called)
	assertNoCall(s, called)
}

// ActionRetryable → NakWithDelay(): message is redelivered with NumDelivered incremented.
func (s *TimerStreamTestSuite) TestRetryable_MessageRedelivered() {
	deliveries := make(chan uint64, 10)
	var callCount atomic.Int32
	handler := func(_ context.Context, _ ext.Timer, numDelivered uint64) intr.HandleResult {
		count := callCount.Add(1)
		deliveries <- numDelivered
		if count == 1 {
			return intr.Retryable(10 * time.Millisecond)
		}
		return intr.Processed()
	}

	s.publishFiredTimer(s.makeTimer(ext.TimerKindSleep))
	ts := s.startStream(handler)
	defer ts.Stop()

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
func (s *TimerStreamTestSuite) TestMalformed_HandlerNeverCalled() {
	called := make(chan struct{}, 1)
	handler := func(_ context.Context, _ ext.Timer, _ uint64) intr.HandleResult {
		called <- struct{}{}
		return intr.Processed()
	}

	_, err := s.js.Publish(context.Background(), natsreg.Manifest.TimerFiredSubject(), []byte{0xFF, 0xFE, 0x00, 0x01})
	s.Require().NoError(err)

	ts := s.startStream(handler)
	defer ts.Stop()

	assertNoCall(s, called)
}

// The handler receives the exact timer that was published.
func (s *TimerStreamTestSuite) TestCorrectTimer_PassedToHandler() {
	received := make(chan ext.Timer, 1)
	handler := func(_ context.Context, t ext.Timer, _ uint64) intr.HandleResult {
		received <- t
		return intr.Processed()
	}

	expected := s.makeTimer(ext.TimerKindStepTimeout)
	s.publishFiredTimer(expected)
	ts := s.startStream(handler)
	defer ts.Stop()

	got := waitForTimer(s, received)
	s.Equal(expected.ID, got.ID)
	s.Equal(expected.Kind, got.Kind)
	s.Equal(expected.WFID, got.WFID)
}

// GetTimer on a fresh stream returns an empty timer (zero ID).
func (s *TimerStreamTestSuite) TestGetTimer_ReturnsEmpty_WhenNotSet() {
	ts, err := NewTimerStream(context.Background(), s.js, s.stream)
	s.Require().NoError(err)

	timerID := ext.NewTimerID()
	got, err := ts.GetTimer(context.Background(), s.wfID, ext.TimerKindSleep, timerID)
	s.Require().NoError(err)
	s.Equal(ext.TimerID(""), got.ID)
}

// AddTimer stores a timer that GetTimer can retrieve.
func (s *TimerStreamTestSuite) TestAddTimer_TimerIsRetrievable() {
	ts, err := NewTimerStream(context.Background(), s.js, s.stream)
	s.Require().NoError(err)

	timer := s.makeTimer(ext.TimerKindSleep)
	expiresAt := time.Now().UTC().Add(time.Minute)

	err = ts.AddTimer(context.Background(), timer, expiresAt)
	s.Require().NoError(err)

	got, err := ts.GetTimer(context.Background(), s.wfID, ext.TimerKindSleep, timer.ID)
	s.Require().NoError(err)
	s.Equal(timer.ID, got.ID)
	s.Equal(timer.Kind, got.Kind)
}

// CancelTimer removes the stored timer so GetTimer returns empty afterward.
func (s *TimerStreamTestSuite) TestCancelTimer_RemovesStoredTimer() {
	ts, err := NewTimerStream(context.Background(), s.js, s.stream)
	s.Require().NoError(err)

	timer := s.makeTimer(ext.TimerKindWaitEventTimeout)
	expiresAt := time.Now().UTC().Add(time.Minute)

	err = ts.AddTimer(context.Background(), timer, expiresAt)
	s.Require().NoError(err)

	got, err := ts.GetTimer(context.Background(), s.wfID, ext.TimerKindWaitEventTimeout, timer.ID)
	s.Require().NoError(err)
	s.NotEmpty(got.ID)

	err = ts.CancelTimer(context.Background(), s.wfID, ext.TimerKindWaitEventTimeout, timer.ID)
	s.Require().NoError(err)

	got, err = ts.GetTimer(context.Background(), s.wfID, ext.TimerKindWaitEventTimeout, timer.ID)
	s.Require().NoError(err)
	s.Equal(ext.TimerID(""), got.ID)
}

// CancelTimer on a non-existent subject succeeds without error.
func (s *TimerStreamTestSuite) TestCancelTimer_Idempotent() {
	ts, err := NewTimerStream(context.Background(), s.js, s.stream)
	s.Require().NoError(err)

	timerID := ext.NewTimerID()
	err = ts.CancelTimer(context.Background(), s.wfID, ext.TimerKindSleep, timerID)
	s.Require().NoError(err)
}

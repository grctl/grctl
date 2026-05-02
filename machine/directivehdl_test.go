//go:build integration

package machine_test

import (
	"context"
	"grctl/server/machine"
	"grctl/server/store"
	"grctl/server/testutil"
	ext "grctl/server/types/external/v1"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/suite"
)

type DirectiveHandlerTestSuite struct {
	suite.Suite
	nc      *nats.Conn
	ns      *server.Server
	store   *store.StateStore
	handler *machine.DirectiveHandler
	wfID    ext.WFID
	runID   ext.RunID
	wfType  ext.WFType
}

func (s *DirectiveHandlerTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns = nc, ns

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)

	s.store = store.NewStateStore(js, stream)
	s.handler = machine.NewDirectiveHandler(s.store)
	s.wfID = ext.NewWFID()
	s.runID = ext.NewRunID()
	s.wfType = ext.NewWFType("test")
}

func (s *DirectiveHandlerTestSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestDirectiveHandler(t *testing.T) {
	suite.Run(t, new(DirectiveHandlerTestSuite))
}

func (s *DirectiveHandlerTestSuite) makeEventDirective() ext.Directive {
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

// seedInboxEvent stores an event directive in the inbox and returns the stored directive
// (with EventSeqID populated) and its NATS-assigned sequence number.
func (s *DirectiveHandlerTestSuite) seedInboxEvent() (ext.Directive, uint64) {
	ctx := context.Background()
	d := s.makeEventDirective()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.InboxUpdate{Directive: d},
	})
	s.Require().NoError(err)

	event, seqID, err := s.store.GetNextEvent(ctx, s.wfID, 0)
	s.Require().NoError(err)

	return event, seqID
}

// Incoming event while the run is in WaitEvent state
// with an event already in the inbox — dispatches the inbox event
// and transitions the run to Step with LastEventSeqID pointing at the dispatched event.
func (s *DirectiveHandlerTestSuite) TestEventWhileWaitingDispatches() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunStateUpdate{
			State: ext.RunState{
				Kind:  ext.RunStateWaitEvent,
				WFID:  s.wfID,
				RunID: s.runID,
			},
		},
	})
	s.Require().NoError(err)

	// event_A is already queued in the inbox; capture its NATS-assigned seqID
	_, eventASeqID := s.seedInboxEvent()

	// event_B arrives: should trigger dispatch of event_A and transition to Step
	eventB := s.makeEventDirective()
	result := s.handler.Handle(ctx, eventB, 1)

	s.True(result.IsProcessed())

	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.Equal(ext.RunStateStep, snapshot.RunState.Kind)
	// LastEventSeqID must point at event_A, proving event_A was the dispatched event.
	s.Equal(eventASeqID, snapshot.RunState.LastEventSeqID)
}

// Incoming event while the run is in WaitEvent state with an empty inbox
// is first stored, then dispatched from the inbox with its assigned seqID.
func (s *DirectiveHandlerTestSuite) TestFirstEventWhileWaitingDispatches() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunStateUpdate{
			State: ext.RunState{
				Kind:  ext.RunStateWaitEvent,
				WFID:  s.wfID,
				RunID: s.runID,
			},
		},
	})
	s.Require().NoError(err)

	event := s.makeEventDirective()
	result := s.handler.Handle(ctx, event, 1)

	s.True(result.IsProcessed())

	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.Equal(ext.RunStateStep, snapshot.RunState.Kind)
	s.NotZero(snapshot.RunState.LastEventSeqID)
}

// an event arriving while the run is in
// Step state is stored in the inbox without triggering any state transition.
func (s *DirectiveHandlerTestSuite) TestEventWhileStepStoredInInbox() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunStateUpdate{
			State: ext.RunState{
				Kind:  ext.RunStateStep,
				WFID:  s.wfID,
				RunID: s.runID,
			},
		},
	})
	s.Require().NoError(err)

	eventD := s.makeEventDirective()
	result := s.handler.Handle(ctx, eventD, 1)

	s.True(result.IsProcessed())

	// Run state must be unchanged
	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.Equal(ext.RunStateStep, snapshot.RunState.Kind)

	// Event must have been persisted in the inbox
	storedEvent, _, err := s.store.GetNextEvent(ctx, s.wfID, 0)
	s.Require().NoError(err)
	s.Equal(ext.DirectiveKindEvent, storedEvent.Kind)
}

// WaitEvent directive arriving while an event is already in the inbox
// immediately dispatches the inbox event and transitions the run to Step state instead of waiting.
func (s *DirectiveHandlerTestSuite) TestWaitEventWithInboxEventDispatches() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunStateUpdate{
			State: ext.RunState{
				Kind:  ext.RunStateStep,
				WFID:  s.wfID,
				RunID: s.runID,
			},
		},
	})
	s.Require().NoError(err)

	// Seed an event in the inbox before the WaitEvent directive arrives
	_, eventSeqID := s.seedInboxEvent()

	waitEventD := ext.Directive{
		ID:   ext.NewDirectiveID(),
		Kind: ext.DirectiveKindWaitEvent,
		RunInfo: ext.RunInfo{
			WFID:   s.wfID,
			ID:     s.runID,
			WFType: s.wfType,
		},
		Msg: &ext.WaitEvent{},
	}
	result := s.handler.Handle(ctx, waitEventD, 1)

	s.True(result.IsProcessed())

	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.Equal(ext.RunStateStep, snapshot.RunState.Kind)
	// LastEventSeqID must point at the inbox event, proving it was dispatched.
	s.Equal(eventSeqID, snapshot.RunState.LastEventSeqID)
}

// Any directive kind arriving after the run has reached a terminal state
// is silently dropped — the run must not transition out of terminal.
func (s *DirectiveHandlerTestSuite) TestDirectiveHandler_RejectsDirectiveWhenRunTerminal() {
	terminalKinds := []ext.RunStateKind{
		ext.RunStateComplete,
		ext.RunStateFail,
		ext.RunStateCancel,
	}

	directives := []ext.Directive{
		{
			ID:      ext.NewDirectiveID(),
			Kind:    ext.DirectiveKindEvent,
			RunInfo: ext.RunInfo{WFID: s.wfID, ID: s.runID, WFType: s.wfType},
			Msg:     &ext.Event{EventName: "test-event"},
		},
		{
			ID:      ext.NewDirectiveID(),
			Kind:    ext.DirectiveKindCancel,
			RunInfo: ext.RunInfo{WFID: s.wfID, ID: s.runID, WFType: s.wfType},
			Msg:     &ext.Cancel{Reason: "test"},
		},
		{
			ID:      ext.NewDirectiveID(),
			Kind:    ext.DirectiveKindStepTimeout,
			RunInfo: ext.RunInfo{WFID: s.wfID, ID: s.runID, WFType: s.wfType},
			Msg:     &ext.StepTimeout{StepName: "test-step"},
		},
	}

	for _, terminalKind := range terminalKinds {
		for _, d := range directives {
			s.Run(string(terminalKind)+"/"+string(d.Kind), func() {
				ctx := context.Background()

				err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
					store.RunStateUpdate{
						State: ext.RunState{
							Kind:  terminalKind,
							WFID:  s.wfID,
							RunID: s.runID,
						},
					},
				})
				s.Require().NoError(err)

				result := s.handler.Handle(ctx, d, 1)
				s.True(result.IsProcessed())

				snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
				s.Require().NoError(err)
				s.Equal(terminalKind, snapshot.RunState.Kind)
			})
		}
	}
}

// delivering the same directive twice is a no-op on the second delivery
// leaving the run in its post-first-delivery state.
func (s *DirectiveHandlerTestSuite) TestDuplicateEventIsIdempotent() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunStateUpdate{
			State: ext.RunState{
				Kind:  ext.RunStateWaitEvent,
				WFID:  s.wfID,
				RunID: s.runID,
			},
		},
	})
	s.Require().NoError(err)

	// event_A is already queued in the inbox
	_, _ = s.seedInboxEvent()

	// Deliver event_B twice (same directive ID, different numDelivered)
	eventB := s.makeEventDirective()
	result1 := s.handler.Handle(ctx, eventB, 1)
	result2 := s.handler.Handle(ctx, eventB, 2)

	s.True(result1.IsProcessed())
	s.True(result2.IsProcessed())

	// Final state must be Step with no double transition
	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.Equal(ext.RunStateStep, snapshot.RunState.Kind)
}

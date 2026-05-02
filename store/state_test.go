//go:build integration

package store_test

import (
	"context"
	"grctl/server/store"
	"grctl/server/testutil"
	ext "grctl/server/types/external/v1"
	"testing"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/suite"
)

type GetStateSnapshotTestSuite struct {
	suite.Suite
	nc     *nats.Conn
	ns     *server.Server
	store  *store.StateStore
	wfID   ext.WFID
	runID  ext.RunID
	wfType ext.WFType
}

func (s *GetStateSnapshotTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns = nc, ns

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)

	s.store = store.NewStateStore(js, stream)
	s.wfID = ext.NewWFID()
	s.runID = ext.NewRunID()
	s.wfType = ext.NewWFType("test")
}

func (s *GetStateSnapshotTestSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestGetStateSnapshot(t *testing.T) {
	suite.Run(t, new(GetStateSnapshotTestSuite))
}

func (s *GetStateSnapshotTestSuite) makeEventDirective() ext.Directive {
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

func (s *GetStateSnapshotTestSuite) makeCancelDirective() ext.Directive {
	return ext.Directive{
		ID:   ext.NewDirectiveID(),
		Kind: ext.DirectiveKindCancel,
		RunInfo: ext.RunInfo{
			WFID:   s.wfID,
			ID:     s.runID,
			WFType: s.wfType,
		},
		Msg: &ext.Cancel{},
	}
}

// TestWaitEventWithEvent verifies that concurrent RunState and Event fetches
// both populate the snapshot when the run is in WaitEvent state.
func (s *GetStateSnapshotTestSuite) TestWaitEventWithEvent() {
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

	err = s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.InboxUpdate{Directive: s.makeEventDirective()},
	})
	s.Require().NoError(err)

	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)

	s.Equal(ext.RunStateWaitEvent, snapshot.RunState.Kind)
	s.Equal(ext.DirectiveKindEvent, snapshot.Event.Kind)
	s.Empty(snapshot.Cancel.Kind)
}

// TestEventCursorSkipsSeen verifies that LastEventSeqID correctly filters
// already-seen events and returns only the next one via GetBatch startAfterSeq.
func (s *GetStateSnapshotTestSuite) TestEventCursorSkipsSeen() {
	ctx := context.Background()

	// Publish event1
	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.InboxUpdate{Directive: s.makeEventDirective()},
	})
	s.Require().NoError(err)

	// Read event1 back to get its seqID
	event1, event1SeqID, err := s.store.GetNextEvent(ctx, s.wfID, 0)
	s.Require().NoError(err)
	s.Require().NotNil(event1.Msg.(*ext.Event).EventSeqID)

	// Publish event2
	err = s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.InboxUpdate{Directive: s.makeEventDirective()},
	})
	s.Require().NoError(err)

	// Publish RunState with LastEventSeqID pointing at event1 — snapshot should skip event1
	err = s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunStateUpdate{
			State: ext.RunState{
				Kind:           ext.RunStateWaitEvent,
				WFID:           s.wfID,
				RunID:          s.runID,
				LastEventSeqID: event1SeqID,
			},
		},
	})
	s.Require().NoError(err)

	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)

	s.Equal(ext.DirectiveKindEvent, snapshot.Event.Kind)
	// The returned event must not be event1
	gotSeqID := snapshot.Event.Msg.(*ext.Event).EventSeqID
	s.Require().NotNil(gotSeqID)
	s.NotEqual(event1SeqID, *gotSeqID)
}

// TestCancelSuppressesEvent verifies that a Cancel directive prevents
// GetNextEvent from being called, leaving Event empty in the snapshot.
func (s *GetStateSnapshotTestSuite) TestCancelSuppressesEvent() {
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

	err = s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.InboxUpdate{Directive: s.makeEventDirective()},
	})
	s.Require().NoError(err)

	err = s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.InboxUpdate{Directive: s.makeCancelDirective()},
	})
	s.Require().NoError(err)

	snapshot, err := s.store.GetStateSnapshot(ctx, s.wfID, s.runID)
	s.Require().NoError(err)

	s.Equal(ext.DirectiveKindCancel, snapshot.Cancel.Kind)
	s.Empty(snapshot.Event.Kind)
}

// HasRunDataTestSuite tests HasRunInput, HasRunOutput, and HasRunError.
type HasRunDataTestSuite struct {
	suite.Suite
	nc    *nats.Conn
	ns    *server.Server
	store *store.StateStore
	wfID  ext.WFID
	runID ext.RunID
}

func (s *HasRunDataTestSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns = nc, ns

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)

	s.store = store.NewStateStore(js, stream)
	s.wfID = ext.NewWFID()
	s.runID = ext.NewRunID()
}

func (s *HasRunDataTestSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestHasRunData(t *testing.T) {
	suite.Run(t, new(HasRunDataTestSuite))
}

func (s *HasRunDataTestSuite) TestHasRunInput_True() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunInputUpdate{WFID: s.wfID, RunID: s.runID, Input: "test-input"},
	})
	s.Require().NoError(err)

	has, err := s.store.HasRunInput(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.True(has)
}

func (s *HasRunDataTestSuite) TestHasRunInput_False() {
	ctx := context.Background()

	has, err := s.store.HasRunInput(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.False(has)
}

func (s *HasRunDataTestSuite) TestHasRunOutput_True() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunOutputUpdate{WFID: s.wfID, RunID: s.runID, Result: "test-output"},
	})
	s.Require().NoError(err)

	has, err := s.store.HasRunOutput(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.True(has)
}

func (s *HasRunDataTestSuite) TestHasRunError_True() {
	ctx := context.Background()

	err := s.store.ApplyStateUpdates(ctx, []store.StateUpdate{
		store.RunErrorUpdate{
			WFID:  s.wfID,
			RunID: s.runID,
			Error: ext.ErrorDetails{Message: "something failed"},
		},
	})
	s.Require().NoError(err)

	has, err := s.store.HasRunError(ctx, s.wfID, s.runID)
	s.Require().NoError(err)
	s.True(has)
}

func TestEnsureStateStream_CancelledContext_ReturnsError(t *testing.T) {
	nc, js, ns, err := testutil.RunEmbeddedNATS(t.TempDir())
	if err != nil {
		t.Fatalf("failed to start embedded NATS: %v", err)
	}
	defer nc.Close()
	defer ns.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = store.EnsureStateStream(ctx, js, true)
	if err == nil {
		t.Fatal("expected error from EnsureStateStream with cancelled context")
	}
}

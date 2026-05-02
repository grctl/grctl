//go:build integration

package api_test

import (
	"context"
	"testing"
	"time"

	"grctl/server/config"
	"grctl/server/natsreg"
	"grctl/server/server"
	"grctl/server/store"
	"grctl/server/testutil"
	ext "grctl/server/types/external/v1"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/suite"
	"github.com/vmihailenco/msgpack/v5"
)

type TerminalCleanupSuite struct {
	suite.Suite
	nc         *nats.Conn
	ns         *natsserver.Server
	srv        *server.Server
	stateStore *store.StateStore
	stream     jetstream.Stream
}

func TestTerminalCleanup(t *testing.T) {
	suite.Run(t, new(TerminalCleanupSuite))
}

func (s *TerminalCleanupSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc = nc
	s.ns = ns

	cfg := &config.Config{
		Defaults: config.DefaultsConfig{
			WorkerResponseTimeout: 5 * time.Second,
			StepTimeout:           5 * time.Minute,
		},
	}

	srv, err := server.NewServer(context.Background(), nc, js, cfg, &server.Options{InMemory: true})
	s.Require().NoError(err)
	s.Require().NoError(srv.Start())
	s.srv = srv

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)
	s.stream = stream
	s.stateStore = store.NewStateStore(js, stream)
}

func (s *TerminalCleanupSuite) TearDownTest() {
	_ = s.srv.Stop(context.Background())
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func (s *TerminalCleanupSuite) startRun(wfID ext.WFID, runID ext.RunID, wfType ext.WFType) {
	cmd := ext.Command{
		ID:        ext.NewCmdID(),
		Kind:      ext.CmdKindRunStart,
		Timestamp: time.Now().UTC(),
		Msg: &ext.StartCmd{
			RunInfo: ext.RunInfo{
				ID:     runID,
				WFID:   wfID,
				WFType: wfType,
			},
		},
	}
	data, err := msgpack.Marshal(&cmd)
	s.Require().NoError(err)
	_, err = s.nc.Request(natsreg.Manifest.APISubject(wfID), data, 3*time.Second)
	s.Require().NoError(err)
}

func (s *TerminalCleanupSuite) subjectEmpty(subject string) bool {
	_, err := s.stream.GetLastMsgForSubject(context.Background(), subject)
	return err != nil
}

func (s *TerminalCleanupSuite) waitForRunStateKind(ctx context.Context, wfID ext.WFID, runID ext.RunID, kind ext.RunStateKind) ext.RunState {
	s.T().Helper()
	var state ext.RunState
	s.Require().Eventually(func() bool {
		st, err := s.stateStore.GetRunState(ctx, wfID, runID)
		if err != nil {
			return false
		}
		state = st
		return st.Kind == kind
	}, 3*time.Second, 50*time.Millisecond, "timed out waiting for run state kind %s", kind)
	return state
}

// TestFailedRunIsCleaned verifies that forcing a run failure causes the background task to
// purge all wfID-scoped residue subjects while preserving the durable run record.
func (s *TerminalCleanupSuite) TestFailedRunIsCleaned() {
	ctx := context.Background()
	wfID := ext.NewWFID()
	runID := ext.NewRunID()
	wfType := ext.NewWFType("test-workflow")

	s.startRun(wfID, runID, wfType)

	// Capture the step state to derive the timer ID before it is cleared on terminal transition.
	stepState := s.waitForRunStateKind(ctx, wfID, runID, ext.RunStateStep)

	directiveSubject := natsreg.Manifest.DirectiveSubject(wfType, wfID, runID)
	workerTaskSubject := natsreg.Manifest.WorkerTaskSubject(wfType, wfID, runID)
	timerID := ext.DeriveTimerID(stepState.ActiveDirectiveID, ext.TimerKindStepTimeout)
	timerSubject := natsreg.Manifest.TimerSubject(wfID, ext.TimerKindStepTimeout, timerID)

	runInfo, _, err := s.stateStore.GetRunByWFID(ctx, wfID)
	s.Require().NoError(err)

	failDirective := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Kind:      ext.DirectiveKindFail,
		Timestamp: time.Now().UTC(),
		RunInfo:   runInfo,
		Msg: &ext.Fail{
			Error: ext.ErrorDetails{
				Type:    "TestFailure",
				Message: "forced failure in test",
			},
		},
	}
	s.Require().NoError(s.stateStore.PublishDirective(ctx, failDirective))

	s.waitForRunStateKind(ctx, wfID, runID, ext.RunStateFail)

	s.Require().Eventually(func() bool {
		return s.subjectEmpty(directiveSubject) &&
			s.subjectEmpty(workerTaskSubject) &&
			s.subjectEmpty(timerSubject)
	}, 5*time.Second, 100*time.Millisecond, "expected all residue subjects to be purged after run failure")

	// Durable record must survive the purge.
	history, err := s.stateStore.GetHistoryForRun(ctx, wfID, runID)
	s.Require().NoError(err)
	kinds := collectHistoryKinds(history)
	s.Contains(kinds, ext.HistoryKindRunStarted)
	s.Contains(kinds, ext.HistoryKindRunFailed)
}

// TestStepTimeoutIsCleaned verifies that a step timeout transitions the run to failed and then
// triggers cleanup of all wfID-scoped residue while preserving history.
func (s *TerminalCleanupSuite) TestStepTimeoutIsCleaned() {
	ctx := context.Background()
	wfID := ext.NewWFID()
	runID := ext.NewRunID()
	wfType := ext.NewWFType("test-workflow")

	s.startRun(wfID, runID, wfType)

	stepState := s.waitForRunStateKind(ctx, wfID, runID, ext.RunStateStep)

	directiveSubject := natsreg.Manifest.DirectiveSubject(wfType, wfID, runID)
	workerTaskSubject := natsreg.Manifest.WorkerTaskSubject(wfType, wfID, runID)
	timerID := ext.DeriveTimerID(stepState.ActiveDirectiveID, ext.TimerKindStepTimeout)
	timerSubject := natsreg.Manifest.TimerSubject(wfID, ext.TimerKindStepTimeout, timerID)

	runInfo, _, err := s.stateStore.GetRunByWFID(ctx, wfID)
	s.Require().NoError(err)

	// Inject the step_timeout directive directly — this is what the timer stream fires
	// in production after the timer expires.
	timeoutDirective := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Kind:      ext.DirectiveKindStepTimeout,
		Timestamp: time.Now().UTC(),
		RunInfo:   runInfo,
		Msg: &ext.StepTimeout{
			StepName:            "start",
			OriginalDirectiveID: stepState.ActiveDirectiveID,
		},
	}
	s.Require().NoError(s.stateStore.PublishDirective(ctx, timeoutDirective))

	s.waitForRunStateKind(ctx, wfID, runID, ext.RunStateFail)

	s.Require().Eventually(func() bool {
		return s.subjectEmpty(directiveSubject) &&
			s.subjectEmpty(workerTaskSubject) &&
			s.subjectEmpty(timerSubject)
	}, 5*time.Second, 100*time.Millisecond, "expected all residue subjects to be purged after step timeout")

	// Durable record must survive with the full timeout + failure history.
	history, err := s.stateStore.GetHistoryForRun(ctx, wfID, runID)
	s.Require().NoError(err)
	kinds := collectHistoryKinds(history)
	s.Contains(kinds, ext.HistoryKindRunStarted)
	s.Contains(kinds, ext.HistoryKindStepTimeout)
	s.Contains(kinds, ext.HistoryKindRunFailed)
}

func collectHistoryKinds(history []*ext.HistoryEvent) []ext.HistoryKind {
	kinds := make([]ext.HistoryKind, len(history))
	for i, h := range history {
		kinds[i] = h.Kind
	}
	return kinds
}

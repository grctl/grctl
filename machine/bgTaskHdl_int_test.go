//go:build integration

package machine_test

import (
	"context"
	"testing"

	"grctl/server/machine"
	"grctl/server/natsreg"
	"grctl/server/store"
	"grctl/server/testutil"
	intr "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/suite"
	"github.com/vmihailenco/msgpack/v5"
)

type PurgeRunResidueIntegrationSuite struct {
	suite.Suite
	nc     *nats.Conn
	ns     *server.Server
	js     jetstream.JetStream
	stream jetstream.Stream
	store  *store.StateStore
}

func (s *PurgeRunResidueIntegrationSuite) SetupTest() {
	nc, js, ns, err := testutil.RunEmbeddedNATS(s.T().TempDir())
	s.Require().NoError(err)
	s.nc, s.ns, s.js = nc, ns, js

	stream, err := store.EnsureStateStream(context.Background(), js, true)
	s.Require().NoError(err)
	s.stream = stream
	s.store = store.NewStateStore(js, stream)
}

func (s *PurgeRunResidueIntegrationSuite) TearDownTest() {
	s.nc.Close()
	s.ns.Shutdown()
	s.ns.WaitForShutdown()
}

func TestPurgeRunResidue(t *testing.T) {
	suite.Run(t, new(PurgeRunResidueIntegrationSuite))
}

func (s *PurgeRunResidueIntegrationSuite) publishRaw(subject string, data []byte) {
	_, err := s.js.Publish(context.Background(), subject, data)
	s.Require().NoError(err)
}

func (s *PurgeRunResidueIntegrationSuite) subjectEmpty(subject string) bool {
	_, err := s.stream.GetLastMsgForSubject(context.Background(), subject)
	return err != nil
}

func (s *PurgeRunResidueIntegrationSuite) runHandler(wfID ext.WFID) {
	handler := machine.NewBgTaskHandler(nil, nil, s.store, 5)
	task, err := ext.NewPurgeRunResidueTask(ext.NewDirectiveID(), wfID)
	s.Require().NoError(err)

	result := handler.Handle(context.Background(), task, 1)
	s.Equal(intr.Processed(), result)
}

func (s *PurgeRunResidueIntegrationSuite) TestRemovesAllSubjectFamilies() {
	wfID := ext.WFID("wf-target")
	wfType := ext.NewWFType("test")
	runID := ext.NewRunID()
	timerID := ext.TimerID("t1")
	otherWFID := ext.WFID("wf-other")

	payload := []byte("data")

	// Populate all 5 subject families for the target wfID.
	s.publishRaw(natsreg.Manifest.DirectiveSubject(wfType, wfID, runID), payload)
	s.publishRaw(natsreg.Manifest.TimerSubject(wfID, ext.TimerKindStepTimeout, timerID), payload)
	s.publishRaw(natsreg.Manifest.CancelInboxSubject(wfID), payload)
	s.publishRaw(natsreg.Manifest.EventInboxSubject(wfID), payload)
	s.publishRaw(natsreg.Manifest.WorkerTaskSubject(wfType, wfID, runID), payload)

	// Populate a different wfID to confirm it's untouched.
	s.publishRaw(natsreg.Manifest.DirectiveSubject(wfType, otherWFID, runID), payload)

	s.runHandler(wfID)

	s.True(s.subjectEmpty(natsreg.Manifest.DirectiveSubject(wfType, wfID, runID)), "directive subject should be empty")
	s.True(s.subjectEmpty(natsreg.Manifest.TimerSubject(wfID, ext.TimerKindStepTimeout, timerID)), "timer subject should be empty")
	s.True(s.subjectEmpty(natsreg.Manifest.CancelInboxSubject(wfID)), "cancel inbox should be empty")
	s.True(s.subjectEmpty(natsreg.Manifest.EventInboxSubject(wfID)), "event inbox should be empty")
	s.True(s.subjectEmpty(natsreg.Manifest.WorkerTaskSubject(wfType, wfID, runID)), "worker task subject should be empty")

	// Other wfID untouched.
	s.False(s.subjectEmpty(natsreg.Manifest.DirectiveSubject(wfType, otherWFID, runID)), "other wfID directive should still exist")
}

func (s *PurgeRunResidueIntegrationSuite) TestIdempotent() {
	wfID := ext.WFID("wf-idempotent")
	wfType := ext.NewWFType("test")
	runID := ext.NewRunID()

	s.publishRaw(natsreg.Manifest.DirectiveSubject(wfType, wfID, runID), []byte("data"))

	handler := machine.NewBgTaskHandler(nil, nil, s.store, 5)
	task, err := ext.NewPurgeRunResidueTask(ext.NewDirectiveID(), wfID)
	s.Require().NoError(err)

	result1 := handler.Handle(context.Background(), task, 1)
	result2 := handler.Handle(context.Background(), task, 2)

	s.Equal(intr.Processed(), result1)
	s.Equal(intr.Processed(), result2)
}

func (s *PurgeRunResidueIntegrationSuite) TestNoMessages() {
	wfID := ext.WFID("wf-empty")

	handler := machine.NewBgTaskHandler(nil, nil, s.store, 5)
	task, err := ext.NewPurgeRunResidueTask(ext.NewDirectiveID(), wfID)
	s.Require().NoError(err)

	result := handler.Handle(context.Background(), task, 1)
	s.Equal(intr.Processed(), result)
}

func (s *PurgeRunResidueIntegrationSuite) TestPreservesDurableRecord() {
	wfID := ext.WFID("wf-durable")
	wfType := ext.NewWFType("test")
	runID := ext.NewRunID()

	// Publish durable record subjects (these must survive the purge).
	historySubject := natsreg.Manifest.HistorySubject(wfID, runID)
	runStateSubject := natsreg.Manifest.RunStateSubject(wfID, runID)
	runInfoKey := natsreg.Manifest.RunInfoKey(wfType, wfID, runID)

	data, err := msgpack.Marshal(map[string]string{"key": "value"})
	s.Require().NoError(err)
	s.publishRaw(historySubject, data)
	s.publishRaw(runStateSubject, data)
	s.publishRaw(runInfoKey, data)

	// Also publish a directive (should be purged).
	s.publishRaw(natsreg.Manifest.DirectiveSubject(wfType, wfID, runID), []byte("directive"))

	s.runHandler(wfID)

	s.False(s.subjectEmpty(historySubject), "history should be preserved")
	s.False(s.subjectEmpty(runStateSubject), "run state should be preserved")
	s.False(s.subjectEmpty(runInfoKey), "run info should be preserved")
	s.True(s.subjectEmpty(natsreg.Manifest.DirectiveSubject(wfType, wfID, runID)), "directive should be purged")
}

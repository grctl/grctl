package jsstore

import (
	"grctl/server/natsreg"
	models "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type Record2NatsSuite struct {
	suite.Suite
}

func TestRecord2Nats(t *testing.T) {
	if err := natsreg.Init(); err != nil {
		t.Fatalf("failed to init nats manifest: %v", err)
	}
	suite.Run(t, new(Record2NatsSuite))
}

func (s *Record2NatsSuite) TestRunInfoRecord_Subject() {
	wfType := ext.NewWFType("mytype")
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.RunInfoRecord{
		Info: ext.RunInfo{
			WFType: wfType,
			WFID:   wfID,
			ID:     runID,
		},
	}

	msgs, err := RunInfoRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunInfoKey(wfType, wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestRunInfoRecord_ExpectedSeqHeaderSetWhenNonZero() {
	r := models.RunInfoRecord{
		Info: ext.RunInfo{
			WFType: ext.NewWFType("t"),
			WFID:   ext.NewWFID(),
			ID:     ext.NewRunID(),
		},
		ExpectedSeq: 7,
	}

	msgs, err := RunInfoRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Equal(s.T(), "7", msgs[0].Header.Get("Nats-Expected-Last-Subject-Sequence"))
}

func (s *Record2NatsSuite) TestRunInfoRecord_NoExpectedSeqHeaderWhenZero() {
	r := models.RunInfoRecord{
		Info: ext.RunInfo{
			WFType: ext.NewWFType("t"),
			WFID:   ext.NewWFID(),
			ID:     ext.NewRunID(),
		},
	}

	msgs, err := RunInfoRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Nil(s.T(), msgs[0].Header)
}

func (s *Record2NatsSuite) TestRunInputRecord_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.RunInputRecord{
		WFID:  wfID,
		RunID: runID,
		Input: "hello",
	}

	msgs, err := RunInputRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunInputKey(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestRunOutputRecord_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.RunOutputRecord{
		WFID:   wfID,
		RunID:  runID,
		Result: "result",
	}

	msgs, err := RunOutputRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunOutputKey(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestRunErrorRecord_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.RunErrorRecord{
		WFID:  wfID,
		RunID: runID,
		Error: ext.ErrorDetails{Type: "TestError", Message: "something failed"},
	}

	msgs, err := RunErrorRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunErrorKey(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestRunStateRecord_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.RunStateRecord{
		State: ext.RunState{
			WFID:  wfID,
			RunID: runID,
			Kind:  ext.RunStateIdle,
		},
	}

	msgs, err := RunStateRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunStateSubject(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestRunStateRecord_ExpectedSeqHeader() {
	r := models.RunStateRecord{
		State: ext.RunState{
			WFID:  ext.NewWFID(),
			RunID: ext.NewRunID(),
		},
		ExpectedSeq: 3,
	}

	msgs, err := RunStateRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Equal(s.T(), "3", msgs[0].Header.Get("Nats-Expected-Last-Subject-Sequence"))
}

func (s *Record2NatsSuite) TestRunStateRecord_ErrorOnEmptyWFID() {
	r := models.RunStateRecord{
		State: ext.RunState{
			RunID: ext.NewRunID(),
		},
	}

	_, err := RunStateRecordToNatsMsgs(r)
	require.Error(s.T(), err)
}

func (s *Record2NatsSuite) TestRunStateRecord_ErrorOnEmptyRunID() {
	r := models.RunStateRecord{
		State: ext.RunState{
			WFID: ext.NewWFID(),
		},
	}

	_, err := RunStateRecordToNatsMsgs(r)
	require.Error(s.T(), err)
}

func (s *Record2NatsSuite) TestTimerRecord_Subject() {
	wfID := ext.NewWFID()
	kind := ext.TimerKindStepTimeout
	timerID := ext.NewTimerID()

	r := models.TimerRecord{
		Timer: ext.Timer{
			ID:        timerID,
			WFID:      wfID,
			Kind:      kind,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}

	msgs, err := TimerRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.TimerSubject(wfID, kind, timerID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestTimerRecord_ScheduleHeaderContainsAtAt() {
	r := models.TimerRecord{
		Timer: ext.Timer{
			WFID:      ext.NewWFID(),
			Kind:      ext.TimerKindWaitTimeout,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}

	msgs, err := TimerRecordToNatsMsgs(r)
	require.NoError(s.T(), err)

	schedule := msgs[0].Header.Get("Nats-Schedule")
	require.True(s.T(), strings.HasPrefix(schedule, "@at "), "Nats-Schedule should start with '@at ', got: %s", schedule)
}

func (s *Record2NatsSuite) TestTimerRecord_ScheduleTargetIsSet() {
	r := models.TimerRecord{
		Timer: ext.Timer{
			WFID:      ext.NewWFID(),
			Kind:      ext.TimerKindWaitTimeout,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}

	msgs, err := TimerRecordToNatsMsgs(r)
	require.NoError(s.T(), err)

	target := msgs[0].Header.Get("Nats-Schedule-Target")
	require.Equal(s.T(), natsreg.Manifest.TimerFiredSubject(), target)
}

func (s *Record2NatsSuite) TestHistoryRecord_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.HistoryRecord{
		History: ext.HistoryEvent{
			WFID:  wfID,
			RunID: runID,
			Kind:  ext.HistoryKindRunStarted,
			Msg:   ext.RunStarted{},
		},
	}

	msgs, err := HistoryRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.HistorySubject(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestInboxRecord_EventKindUsesEventInboxSubject() {
	wfID := ext.NewWFID()

	r := models.InboxRecord{
		Directive: ext.Directive{
			Kind: ext.DirectiveKindEvent,
			RunInfo: ext.RunInfo{
				WFID: wfID,
			},
		},
	}

	msgs, err := InboxRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.EventInboxSubject(wfID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestKVRecord_OneMessagePerKey() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.KVRecord{
		WFID:  wfID,
		RunID: runID,
		KVMap: map[string]any{
			"key1": "value1",
			"key2": 42,
		},
	}

	msgs, err := KVRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 2)

	subjects := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		subjects[m.Subject] = true
	}

	require.Contains(s.T(), subjects, natsreg.Manifest.WfKVKey(wfID, runID, "key1"))
	require.Contains(s.T(), subjects, natsreg.Manifest.WfKVKey(wfID, runID, "key2"))
}

func (s *Record2NatsSuite) TestKVRecord_EmptyMapReturnsNoMessages() {
	r := models.KVRecord{
		WFID:  ext.NewWFID(),
		RunID: ext.NewRunID(),
		KVMap: map[string]any{},
	}

	msgs, err := KVRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Empty(s.T(), msgs)
}

func (s *Record2NatsSuite) TestWorkerTaskDispatch_Subject() {
	wfType := ext.NewWFType("mytype")
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	r := models.WorkerTaskDispatch{
		Directive: ext.Directive{
			RunInfo: ext.RunInfo{
				WFType: wfType,
				WFID:   wfID,
				ID:     runID,
			},
		},
	}

	msgs, err := WorkerTaskDispatchToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.WorkerTaskSubject(wfType, wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *Record2NatsSuite) TestBackgroundTaskRecord_Subject() {
	r := models.BackgroundTaskRecord{
		Task: ext.BackgroundTask{
			Kind:            ext.BackgroundTaskKindDeleteTimer,
			DeduplicationID: ext.NewDirectiveID(),
		},
	}

	msgs, err := BackgroundTaskRecordToNatsMsgs(r)
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	require.Equal(s.T(), natsreg.Manifest.BgTaskSubject(), msgs[0].Subject)
}

func (s *Record2NatsSuite) TestBackgroundTaskRecord_DeduplicationIDHeader() {
	dedupID := ext.NewDirectiveID()

	r := models.BackgroundTaskRecord{
		Task: ext.BackgroundTask{
			Kind:            ext.BackgroundTaskKindDeleteTimer,
			DeduplicationID: dedupID,
		},
	}

	msgs, err := BackgroundTaskRecordToNatsMsgs(r)
	require.NoError(s.T(), err)

	require.Equal(s.T(), string(dedupID), msgs[0].Header.Get(nats.MsgIdHdr))
}

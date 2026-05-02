package store

import (
	"grctl/server/natsreg"
	ext "grctl/server/types/external/v1"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type UpdateBldSuite struct {
	suite.Suite
}

func TestUpdateBld(t *testing.T) {
	suite.Run(t, new(UpdateBldSuite))
}

func (s *UpdateBldSuite) TestRunInfoUpdate_Subject() {
	wfType := ext.NewWFType("mytype")
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := RunInfoUpdate{
		Info: ext.RunInfo{
			WFType: wfType,
			WFID:   wfID,
			ID:     runID,
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunInfoKey(wfType, wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestRunInfoUpdate_ExpectedSeqHeaderSetWhenNonZero() {
	u := RunInfoUpdate{
		Info: ext.RunInfo{
			WFType: ext.NewWFType("t"),
			WFID:   ext.NewWFID(),
			ID:     ext.NewRunID(),
		},
		ExpectedSeq: 7,
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Equal(s.T(), "7", msgs[0].Header.Get("Nats-Expected-Last-Subject-Sequence"))
}

func (s *UpdateBldSuite) TestRunInfoUpdate_NoExpectedSeqHeaderWhenZero() {
	u := RunInfoUpdate{
		Info: ext.RunInfo{
			WFType: ext.NewWFType("t"),
			WFID:   ext.NewWFID(),
			ID:     ext.NewRunID(),
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Nil(s.T(), msgs[0].Header)
}

func (s *UpdateBldSuite) TestRunInputUpdate_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := RunInputUpdate{
		WFID:  wfID,
		RunID: runID,
		Input: "hello",
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunInputKey(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestRunOutputUpdate_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := RunOutputUpdate{
		WFID:   wfID,
		RunID:  runID,
		Result: "result",
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunOutputKey(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestRunErrorUpdate_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := RunErrorUpdate{
		WFID:  wfID,
		RunID: runID,
		Error: ext.ErrorDetails{Type: "TestError", Message: "something failed"},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunErrorKey(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestRunStateUpdate_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := RunStateUpdate{
		State: ext.RunState{
			WFID:  wfID,
			RunID: runID,
			Kind:  ext.RunStateIdle,
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.RunStateSubject(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestRunStateUpdate_ExpectedSeqHeader() {
	u := RunStateUpdate{
		State: ext.RunState{
			WFID:  ext.NewWFID(),
			RunID: ext.NewRunID(),
		},
		ExpectedSeq: 3,
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Equal(s.T(), "3", msgs[0].Header.Get("Nats-Expected-Last-Subject-Sequence"))
}

func (s *UpdateBldSuite) TestRunStateUpdate_ErrorOnEmptyWFID() {
	u := RunStateUpdate{
		State: ext.RunState{
			RunID: ext.NewRunID(),
		},
	}

	_, err := u.ToNatsMsgs()
	require.Error(s.T(), err)
}

func (s *UpdateBldSuite) TestRunStateUpdate_ErrorOnEmptyRunID() {
	u := RunStateUpdate{
		State: ext.RunState{
			WFID: ext.NewWFID(),
		},
	}

	_, err := u.ToNatsMsgs()
	require.Error(s.T(), err)
}

func (s *UpdateBldSuite) TestTimerUpdate_Subject() {
	wfID := ext.NewWFID()
	kind := ext.TimerKindStepTimeout
	timerID := ext.NewTimerID()

	u := TimerUpdate{
		Timer: ext.Timer{
			ID:        timerID,
			WFID:      wfID,
			Kind:      kind,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.TimerSubject(wfID, kind, timerID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestTimerUpdate_ScheduleHeaderContainsAtAt() {
	u := TimerUpdate{
		Timer: ext.Timer{
			WFID:      ext.NewWFID(),
			Kind:      ext.TimerKindSleep,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)

	schedule := msgs[0].Header.Get("Nats-Schedule")
	require.True(s.T(), strings.HasPrefix(schedule, "@at "), "Nats-Schedule should start with '@at ', got: %s", schedule)
}

func (s *UpdateBldSuite) TestTimerUpdate_ScheduleTargetIsSet() {
	u := TimerUpdate{
		Timer: ext.Timer{
			WFID:      ext.NewWFID(),
			Kind:      ext.TimerKindSleep,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)

	target := msgs[0].Header.Get("Nats-Schedule-Target")
	require.Equal(s.T(), natsreg.Manifest.TimerFiredSubject(), target)
}

func (s *UpdateBldSuite) TestHistoryUpdate_Subject() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := HistoryUpdate{
		History: ext.HistoryEvent{
			WFID:  wfID,
			RunID: runID,
			Kind:  ext.HistoryKindRunStarted,
			Msg:   ext.RunStarted{},
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.HistorySubject(wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestInboxUpdate_EventKindUsesEventInboxSubject() {
	wfID := ext.NewWFID()

	u := InboxUpdate{
		Directive: ext.Directive{
			Kind: ext.DirectiveKindEvent,
			RunInfo: ext.RunInfo{
				WFID: wfID,
			},
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.EventInboxSubject(wfID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestInboxUpdate_CancelKindUsesCancelInboxSubject() {
	wfID := ext.NewWFID()

	u := InboxUpdate{
		Directive: ext.Directive{
			Kind: ext.DirectiveKindCancel,
			RunInfo: ext.RunInfo{
				WFID: wfID,
			},
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.CancelInboxSubject(wfID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestKVUpdate_OneMessagePerKey() {
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := KVUpdate{
		WFID:  wfID,
		RunID: runID,
		Updates: map[string]any{
			"key1": "value1",
			"key2": 42,
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 2)

	subjects := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		subjects[m.Subject] = true
	}

	require.Contains(s.T(), subjects, natsreg.Manifest.WfKVKey(wfID, runID, "key1"))
	require.Contains(s.T(), subjects, natsreg.Manifest.WfKVKey(wfID, runID, "key2"))
}

func (s *UpdateBldSuite) TestKVUpdate_EmptyUpdatesReturnsNoMessages() {
	u := KVUpdate{
		WFID:    ext.NewWFID(),
		RunID:   ext.NewRunID(),
		Updates: map[string]any{},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Empty(s.T(), msgs)
}

func (s *UpdateBldSuite) TestWorkerTaskDispatch_Subject() {
	wfType := ext.NewWFType("mytype")
	wfID := ext.NewWFID()
	runID := ext.NewRunID()

	u := WorkerTaskDispatch{
		Directive: ext.Directive{
			RunInfo: ext.RunInfo{
				WFType: wfType,
				WFID:   wfID,
				ID:     runID,
			},
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	expected := natsreg.Manifest.WorkerTaskSubject(wfType, wfID, runID)
	require.Equal(s.T(), expected, msgs[0].Subject)
}

func (s *UpdateBldSuite) TestBackgroundTaskUpdate_Subject() {
	u := BackgroundTaskUpdate{
		Task: ext.BackgroundTask{
			Kind:            ext.BackgroundTaskKindDeleteTimer,
			DeduplicationID: ext.NewDirectiveID(),
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)
	require.Len(s.T(), msgs, 1)

	require.Equal(s.T(), natsreg.Manifest.BgTaskSubject(), msgs[0].Subject)
}

func (s *UpdateBldSuite) TestBackgroundTaskUpdate_DeduplicationIDHeader() {
	dedupID := ext.NewDirectiveID()

	u := BackgroundTaskUpdate{
		Task: ext.BackgroundTask{
			Kind:            ext.BackgroundTaskKindDeleteTimer,
			DeduplicationID: dedupID,
		},
	}

	msgs, err := u.ToNatsMsgs()
	require.NoError(s.T(), err)

	require.Equal(s.T(), string(dedupID), msgs[0].Header.Get(nats.MsgIdHdr))
}

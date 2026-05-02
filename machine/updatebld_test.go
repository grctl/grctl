package machine

import (
	"grctl/server/store"
	ext "grctl/server/types/external/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/vmihailenco/msgpack/v5"
)

type UpdateBldSuite struct {
	suite.Suite
	f *UpdateFactory
}

func (s *UpdateBldSuite) SetupTest() {
	s.f = newUpdateFactory()
}

func TestUpdateBld(t *testing.T) {
	suite.Run(t, new(UpdateBldSuite))
}

// --- helpers ---

func ptr[T any](v T) *T {
	return &v
}

func findRunStateUpdate(updates []store.StateUpdate) (store.RunStateUpdate, bool) {
	for _, u := range updates {
		if rsu, ok := u.(store.RunStateUpdate); ok {
			return rsu, true
		}
	}
	return store.RunStateUpdate{}, false
}

func findRunInfoUpdate(updates []store.StateUpdate) (store.RunInfoUpdate, bool) {
	for _, u := range updates {
		if riu, ok := u.(store.RunInfoUpdate); ok {
			return riu, true
		}
	}
	return store.RunInfoUpdate{}, false
}

func findHistoryUpdates(updates []store.StateUpdate) []store.HistoryUpdate {
	var result []store.HistoryUpdate
	for _, u := range updates {
		if hu, ok := u.(store.HistoryUpdate); ok {
			result = append(result, hu)
		}
	}
	return result
}

func makeEventDirective(seqID *uint64) ext.Directive {
	return ext.Directive{
		ID:   ext.NewDirectiveID(),
		Kind: ext.DirectiveKindEvent,
		RunInfo: ext.RunInfo{
			WFID:   ext.NewWFID(),
			WFType: ext.NewWFType("test"),
		},
		Msg: &ext.Event{
			EventSeqID: seqID,
			EventName:  "some-event",
			Timeout:    1000,
		},
	}
}

func makeStepDirective() ext.Directive {
	return ext.Directive{
		ID:   ext.NewDirectiveID(),
		Kind: ext.DirectiveKindStep,
		RunInfo: ext.RunInfo{
			WFID:   ext.NewWFID(),
			WFType: ext.NewWFType("test"),
		},
		Msg: &ext.Step{
			Name:    "some-step",
			Timeout: 1000,
		},
	}
}

func makeStepTimeoutDirective(stepName string, originalDirectiveID ext.DirectiveID) ext.Directive {
	startedAt := time.Now().UTC()
	return ext.Directive{
		ID:   ext.NewDirectiveID(),
		Kind: ext.DirectiveKindStepTimeout,
		RunInfo: ext.RunInfo{
			WFID:      ext.NewWFID(),
			WFType:    ext.NewWFType("test"),
			StartedAt: &startedAt,
		},
		Msg: &ext.StepTimeout{StepName: stepName, OriginalDirectiveID: originalDirectiveID},
	}
}

// --- StartStep ---

func (s *UpdateBldSuite) TestStartStep_EventDirectiveSetsLastEventSeqID() {
	currentState := ext.RunState{Kind: ext.RunStateWaitEvent}
	d := makeEventDirective(ptr(uint64(5)))

	updates, err := s.f.StartStep(d, currentState)
	require.NoError(s.T(), err)

	rsu, found := findRunStateUpdate(updates)
	require.True(s.T(), found, "RunStateUpdate not found in updates")
	require.Equal(s.T(), uint64(5), rsu.State.LastEventSeqID)
}

func (s *UpdateBldSuite) TestStartStep_NonEventDirectivePreservesLastEventSeqID() {
	currentState := ext.RunState{
		Kind:           ext.RunStateIdle,
		LastEventSeqID: 5,
	}
	d := makeStepDirective()

	updates, err := s.f.StartStep(d, currentState)
	require.NoError(s.T(), err)

	rsu, found := findRunStateUpdate(updates)
	require.True(s.T(), found, "RunStateUpdate not found in updates")
	require.Equal(s.T(), uint64(5), rsu.State.LastEventSeqID)
}

func (s *UpdateBldSuite) TestStartStep_EventDirectiveWithNilSeqIDLeavesZero() {
	currentState := ext.RunState{Kind: ext.RunStateWaitEvent}
	d := makeEventDirective(nil)

	updates, err := s.f.StartStep(d, currentState)
	require.NoError(s.T(), err)

	rsu, found := findRunStateUpdate(updates)
	require.True(s.T(), found, "RunStateUpdate not found in updates")
	require.Equal(s.T(), uint64(0), rsu.State.LastEventSeqID)
}

// --- BuildStepTimeout ---

func (s *UpdateBldSuite) TestBuildStepTimeout() {
	stepDirectiveID := ext.NewDirectiveID()
	d := makeStepTimeoutDirective("my-step", stepDirectiveID)
	currentState := ext.RunState{
		Kind:              ext.RunStateStep,
		ActiveDirectiveID: stepDirectiveID,
		SeqID:             7,
	}

	updates, err := s.f.BuildStepTimeout(d, currentState)
	require.NoError(s.T(), err)
	require.Len(s.T(), updates, 6)

	rsu, found := findRunStateUpdate(updates)
	require.True(s.T(), found, "RunStateUpdate not found in updates")
	require.Equal(s.T(), ext.RunStateFail, rsu.State.Kind)
	require.Equal(s.T(), uint64(7), rsu.ExpectedSeq)
	require.False(s.T(), rsu.State.EnteredAt.IsZero())

	riu, found := findRunInfoUpdate(updates)
	require.True(s.T(), found, "RunInfoUpdate not found in updates")
	require.Equal(s.T(), ext.RunStatusFailed, riu.Info.Status)
	require.False(s.T(), riu.Info.CompletedAt.IsZero())

	histUpdates := findHistoryUpdates(updates)
	require.Len(s.T(), histUpdates, 2)

	var stepTimeoutHistory, runFailedHistory *store.HistoryUpdate
	for i := range histUpdates {
		switch histUpdates[i].History.Kind {
		case ext.HistoryKindStepTimeout:
			stepTimeoutHistory = &histUpdates[i]
		case ext.HistoryKindRunFailed:
			runFailedHistory = &histUpdates[i]
		}
	}

	require.NotNil(s.T(), stepTimeoutHistory, "step.timeout history not found")
	stepTimedout, ok := stepTimeoutHistory.History.Msg.(ext.StepTimedout)
	require.True(s.T(), ok, "expected ext.StepTimedout msg")
	require.Equal(s.T(), "my-step", stepTimedout.StepName)

	require.NotNil(s.T(), runFailedHistory, "run.failed history not found")
}

func (s *UpdateBldSuite) TestBuildStepTimeout_ErrorOnWrongMsgType() {
	currentState := ext.RunState{Kind: ext.RunStateIdle}
	d := makeStepDirective()
	d.Kind = ext.DirectiveKindStepTimeout

	_, err := s.f.BuildStepTimeout(d, currentState)
	require.Error(s.T(), err)
}

func (s *UpdateBldSuite) TestBuildStepTimeout_DropsWhenNotInStepState() {
	stepDirectiveID := ext.NewDirectiveID()
	d := makeStepTimeoutDirective("my-step", stepDirectiveID)
	currentState := ext.RunState{
		Kind:              ext.RunStateWaitEvent,
		ActiveDirectiveID: stepDirectiveID,
	}

	_, err := s.f.BuildStepTimeout(d, currentState)
	require.ErrorIs(s.T(), err, ErrStaleDirective)
}

func (s *UpdateBldSuite) TestBuildStepTimeout_DropsWhenDifferentActiveDirective() {
	d := makeStepTimeoutDirective("my-step", ext.NewDirectiveID())
	currentState := ext.RunState{
		Kind:              ext.RunStateStep,
		ActiveDirectiveID: ext.NewDirectiveID(), // different ID
	}

	_, err := s.f.BuildStepTimeout(d, currentState)
	require.ErrorIs(s.T(), err, ErrStaleDirective)
}

func (s *UpdateBldSuite) TestBuildStepTimeout_ActiveDirectiveMatches() {
	stepDirectiveID := ext.NewDirectiveID()
	d := makeStepTimeoutDirective("my-step", stepDirectiveID)
	currentState := ext.RunState{
		Kind:              ext.RunStateStep,
		ActiveDirectiveID: stepDirectiveID,
		SeqID:             3,
	}

	updates, err := s.f.BuildStepTimeout(d, currentState)
	require.NoError(s.T(), err)
	require.Len(s.T(), updates, 6)
}

func (s *UpdateBldSuite) TestBuildStepTimeout_DropsStaleAfterTerminalState() {
	stepDirectiveID := ext.NewDirectiveID()
	d := makeStepTimeoutDirective("my-step", stepDirectiveID)
	currentState := ext.RunState{
		Kind: ext.RunStateComplete,
	}

	_, err := s.f.BuildStepTimeout(d, currentState)
	require.ErrorIs(s.T(), err, ErrStaleDirective)
}

// --- CompleteStep ---

func findBackgroundTaskUpdate(updates []store.StateUpdate) (store.BackgroundTaskUpdate, bool) {
	for _, u := range updates {
		if btu, ok := u.(store.BackgroundTaskUpdate); ok {
			return btu, true
		}
	}
	return store.BackgroundTaskUpdate{}, false
}

func findKVUpdate(updates []store.StateUpdate) (store.KVUpdate, bool) {
	for _, u := range updates {
		if kvu, ok := u.(store.KVUpdate); ok {
			return kvu, true
		}
	}
	return store.KVUpdate{}, false
}

func (s *UpdateBldSuite) TestCompleteStep_AlwaysIncludesHistoryAndTimerCleanup() {
	currentState := ext.RunState{Kind: ext.RunStateStep}
	d := makeStepResultDirective("my-step")

	updates, err := s.f.CompleteStep(d, currentState)
	require.NoError(s.T(), err)
	require.Len(s.T(), updates, 2)

	histUpdates := findHistoryUpdates(updates)
	require.Len(s.T(), histUpdates, 1)
	require.Equal(s.T(), ext.HistoryKindStepCompleted, histUpdates[0].History.Kind)

	_, found := findBackgroundTaskUpdate(updates)
	require.True(s.T(), found, "BackgroundTaskUpdate (timer cleanup) not found")
}

func (s *UpdateBldSuite) TestCompleteStep_WithKVUpdatesIncludesKVUpdate() {
	currentState := ext.RunState{Kind: ext.RunStateStep}
	d := makeStepResultDirective("my-step")
	kvUpdates := ext.KVUpdates{"key": "value"}
	d.Msg.(*ext.StepResult).KVUpdates = &kvUpdates

	updates, err := s.f.CompleteStep(d, currentState)
	require.NoError(s.T(), err)
	require.Len(s.T(), updates, 3)

	kvu, found := findKVUpdate(updates)
	require.True(s.T(), found, "KVUpdate not found")
	require.Equal(s.T(), "value", kvu.Updates["key"])
}

func (s *UpdateBldSuite) TestCompleteStep_ErrorOnWrongMsgType() {
	currentState := ext.RunState{Kind: ext.RunStateStep}
	d := makeStepDirective()
	d.Kind = ext.DirectiveKindStepResult

	_, err := s.f.CompleteStep(d, currentState)
	require.Error(s.T(), err)
}

// --- helpers for input/output/error updates ---

func findRunInputUpdate(updates []store.StateUpdate) (store.RunInputUpdate, bool) {
	for _, u := range updates {
		if riu, ok := u.(store.RunInputUpdate); ok {
			return riu, true
		}
	}
	return store.RunInputUpdate{}, false
}

func findRunOutputUpdate(updates []store.StateUpdate) (store.RunOutputUpdate, bool) {
	for _, u := range updates {
		if rou, ok := u.(store.RunOutputUpdate); ok {
			return rou, true
		}
	}
	return store.RunOutputUpdate{}, false
}

func findRunErrorUpdate(updates []store.StateUpdate) (store.RunErrorUpdate, bool) {
	for _, u := range updates {
		if reu, ok := u.(store.RunErrorUpdate); ok {
			return reu, true
		}
	}
	return store.RunErrorUpdate{}, false
}

func makeStartDirective(input any) ext.Directive {
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindStart,
		RunInfo: ext.RunInfo{
			WFID:   ext.NewWFID(),
			WFType: ext.NewWFType("test"),
			ID:     ext.NewRunID(),
		},
		Msg: &ext.Start{
			Input:   input,
			Timeout: 1000,
		},
	}
}

func makeCompleteDirective(result any) ext.Directive {
	startedAt := time.Now().UTC()
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindComplete,
		RunInfo: ext.RunInfo{
			WFID:      ext.NewWFID(),
			WFType:    ext.NewWFType("test"),
			ID:        ext.NewRunID(),
			StartedAt: &startedAt,
		},
		Msg: &ext.Complete{
			Result: result,
		},
	}
}

func makeFailDirective(errDetails ext.ErrorDetails) ext.Directive {
	startedAt := time.Now().UTC()
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindFail,
		RunInfo: ext.RunInfo{
			WFID:      ext.NewWFID(),
			WFType:    ext.NewWFType("test"),
			ID:        ext.NewRunID(),
			StartedAt: &startedAt,
		},
		Msg: &ext.Fail{
			Error: errDetails,
		},
	}
}

// --- StartRun input ---

func (s *UpdateBldSuite) TestStartRun_WithInputIncludesRunInputUpdate() {
	d := makeStartDirective("hello world")

	updates, err := s.f.StartRun(d)
	require.NoError(s.T(), err)

	riu, found := findRunInputUpdate(updates)
	require.True(s.T(), found, "RunInputUpdate not found in updates")
	require.Equal(s.T(), d.RunInfo.WFID, riu.WFID)
	require.Equal(s.T(), d.RunInfo.ID, riu.RunID)
	require.Equal(s.T(), "hello world", riu.Input)
}

func (s *UpdateBldSuite) TestStartRun_WithNilInputOmitsRunInputUpdate() {
	d := makeStartDirective(nil)

	updates, err := s.f.StartRun(d)
	require.NoError(s.T(), err)

	_, found := findRunInputUpdate(updates)
	require.False(s.T(), found, "RunInputUpdate should not be present when input is nil")
}

// --- CompleteRun output ---

func (s *UpdateBldSuite) TestCompleteRun_WithResultIncludesRunOutputUpdate() {
	d := makeCompleteDirective("some result")
	currentState := ext.RunState{Kind: ext.RunStateStep}

	updates, err := s.f.CompleteRun(d, currentState)
	require.NoError(s.T(), err)

	rou, found := findRunOutputUpdate(updates)
	require.True(s.T(), found, "RunOutputUpdate not found in updates")
	require.Equal(s.T(), d.RunInfo.WFID, rou.WFID)
	require.Equal(s.T(), d.RunInfo.ID, rou.RunID)
	require.Equal(s.T(), "some result", rou.Result)
}

func (s *UpdateBldSuite) TestCompleteRun_WithNilResultOmitsRunOutputUpdate() {
	d := makeCompleteDirective(nil)
	currentState := ext.RunState{Kind: ext.RunStateStep}

	updates, err := s.f.CompleteRun(d, currentState)
	require.NoError(s.T(), err)

	_, found := findRunOutputUpdate(updates)
	require.False(s.T(), found, "RunOutputUpdate should not be present when result is nil")
}

// --- FailRun error ---

func (s *UpdateBldSuite) TestFailRun_IncludesRunErrorUpdate() {
	errDetails := ext.ErrorDetails{Type: "TestError", Message: "something broke"}
	d := makeFailDirective(errDetails)
	currentState := ext.RunState{Kind: ext.RunStateStep}

	updates, err := s.f.FailRun(d, currentState)
	require.NoError(s.T(), err)

	reu, found := findRunErrorUpdate(updates)
	require.True(s.T(), found, "RunErrorUpdate not found in updates")
	require.Equal(s.T(), d.RunInfo.WFID, reu.WFID)
	require.Equal(s.T(), d.RunInfo.ID, reu.RunID)
	require.Equal(s.T(), errDetails, reu.Error)
}

// --- PurgeRunResidue bg-task emission ---

func makeCancelDirective() ext.Directive {
	startedAt := time.Now().UTC()
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindCancel,
		RunInfo: ext.RunInfo{
			WFID:      ext.NewWFID(),
			WFType:    ext.NewWFType("test"),
			ID:        ext.NewRunID(),
			StartedAt: &startedAt,
		},
		Msg: &ext.Cancel{Reason: "test"},
	}
}

func (s *UpdateBldSuite) TestCompleteRun_EmitsPurgeTask() {
	d := makeCompleteDirective(nil)
	currentState := ext.RunState{Kind: ext.RunStateStep}

	updates, err := s.f.CompleteRun(d, currentState)
	require.NoError(s.T(), err)

	btu, found := findBackgroundTaskUpdate(updates)
	require.True(s.T(), found, "BackgroundTaskUpdate not found")
	require.Equal(s.T(), ext.BackgroundTaskKindPurgeRunResidue, btu.Task.Kind)

	var payload ext.PurgeRunResiduePayload
	require.NoError(s.T(), msgpack.Unmarshal(btu.Task.Payload, &payload))
	require.Equal(s.T(), d.RunInfo.WFID, payload.WFID)
}

func (s *UpdateBldSuite) TestFailRun_EmitsPurgeTask() {
	errDetails := ext.ErrorDetails{Type: "TestError", Message: "something broke"}
	d := makeFailDirective(errDetails)
	currentState := ext.RunState{Kind: ext.RunStateStep}

	updates, err := s.f.FailRun(d, currentState)
	require.NoError(s.T(), err)

	btu, found := findBackgroundTaskUpdate(updates)
	require.True(s.T(), found, "BackgroundTaskUpdate not found")
	require.Equal(s.T(), ext.BackgroundTaskKindPurgeRunResidue, btu.Task.Kind)

	var payload ext.PurgeRunResiduePayload
	require.NoError(s.T(), msgpack.Unmarshal(btu.Task.Payload, &payload))
	require.Equal(s.T(), d.RunInfo.WFID, payload.WFID)
}

func (s *UpdateBldSuite) TestCancelRun_EmitsPurgeTask() {
	d := makeCancelDirective()
	currentState := ext.RunState{Kind: ext.RunStateStep}

	updates, err := s.f.CancelRun(d, currentState)
	require.NoError(s.T(), err)

	btu, found := findBackgroundTaskUpdate(updates)
	require.True(s.T(), found, "BackgroundTaskUpdate not found")
	require.Equal(s.T(), ext.BackgroundTaskKindPurgeRunResidue, btu.Task.Kind)

	var payload ext.PurgeRunResiduePayload
	require.NoError(s.T(), msgpack.Unmarshal(btu.Task.Payload, &payload))
	require.Equal(s.T(), d.RunInfo.WFID, payload.WFID)
}

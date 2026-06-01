package run

import (
	"testing"
	"time"

	model "grctl/server/types"
	ext "grctl/server/types/external/v1"

	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// This file is the shared harness for the domain behaviour tests (run/*_test.go).
// Those tests drive plan(directive, snapshot) and assert on the records it emits —
// the full run-observable surface (history, state transition, timers, dispatched
// work, background tasks). See project/system/server_testing.md: the domain test
// suite is the behaviour map, so these helpers are written once and reused per feature.
//
// plan is intentionally non-deterministic in timestamps and generated IDs (the
// determinism cleanup is excluded from the plan-pure-domain ticket), so the matcher
// below deliberately exposes only the semantic fields a behaviour can assert on.

const (
	testWFID  ext.WFID  = "wf-test"
	testRunID ext.RunID = "run-test"
)

// testRunInfo is a running run with a start time set. Several terminal transitions
// (FailRun, CompleteRun) require StartedAt to be non-nil, so the builder always sets it.
func testRunInfo() ext.RunInfo {
	started := time.Now().UTC()
	return ext.RunInfo{
		ID:        testRunID,
		WFID:      testWFID,
		WFType:    "test-workflow",
		Status:    ext.RunStatusRunning,
		CreatedAt: started,
		StartedAt: &started,
	}
}

// emptySnapshot is the state a brand-new run is planned against, before any transition.
func emptySnapshot() model.StateSnapshot {
	return model.StateSnapshot{}
}

// stepSnapshot is a run currently executing a step. activeDirectiveID is the directive
// that drove the run into the step — a step-timeout directive is only honoured when it
// names this same directive.
func stepSnapshot(activeDirectiveID ext.DirectiveID) model.StateSnapshot {
	entered := time.Now().UTC()
	return model.StateSnapshot{
		RunState: ext.RunState{
			WFID:              testWFID,
			RunID:             testRunID,
			Kind:              ext.RunStateStep,
			EnteredAt:         entered,
			StartedAt:         &entered,
			ActiveDirectiveID: activeDirectiveID,
		},
	}
}

// terminalSnapshot is a run that has already reached a terminal state (complete,
// fail, or cancel). Any directive planned against it must be ignored.
func terminalSnapshot(kind ext.RunStateKind) model.StateSnapshot {
	entered := time.Now().UTC()
	return model.StateSnapshot{
		RunState: ext.RunState{
			WFID:      testWFID,
			RunID:     testRunID,
			Kind:      kind,
			EnteredAt: entered,
			StartedAt: &entered,
		},
	}
}

func startDirective(input any) ext.Directive {
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindStart,
		RunInfo:   testRunInfo(),
		Msg:       &ext.Start{Input: input},
	}
}

// withParentCallback marks the directive's run as a child started with a completion
// callback: when the run reaches a terminal state, its outcome is delivered to
// callbackStep in the parent workflow. Returns the directive with those fields set.
func withParentCallback(d ext.Directive, parentWFID ext.WFID, callbackStep string) ext.Directive {
	d.RunInfo.ParentWFID = &parentWFID
	d.RunInfo.ParentCallbackStep = &callbackStep
	return d
}

// cancelDirective is an operator (or parent) requesting the run stop.
func cancelDirective(reason string) ext.Directive {
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindCancel,
		RunInfo:   testRunInfo(),
		Msg:       &ext.Cancel{Reason: reason},
	}
}

// stepResultDirective is a worker reporting that it finished processedStep, with nextMsg
// describing what the workflow does next (run another step, fail the step, complete, …).
func stepResultDirective(processedStep string, nextKind ext.DirectiveKind, nextMsg ext.DirectiveMessage) ext.Directive {
	worker := ext.WorkerID("worker-1")
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindStepResult,
		RunInfo:   testRunInfo(),
		Msg: &ext.StepResult{
			ProcessedMsgKind: ext.DirectiveKindStep,
			ProcessedMsg:     &ext.Step{Name: processedStep},
			WorkerID:         &worker,
			NextMsgKind:      nextKind,
			NextMsg:          nextMsg,
		},
	}
}

// eventDirective is an external event delivered to a run — e.g. a signal arriving
// after the run has already finished.
func eventDirective(name string, payload any) ext.Directive {
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindEvent,
		RunInfo:   testRunInfo(),
		Msg:       &ext.Event{EventName: name, Payload: payload},
	}
}

// stepTimeoutDirective is a fired step-timeout timer translated into its directive.
// originalID must match the snapshot's ActiveDirectiveID for the timeout to be honoured.
func stepTimeoutDirective(stepName string, originalID ext.DirectiveID) ext.Directive {
	return ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindStepTimeout,
		RunInfo:   testRunInfo(),
		Msg: &ext.StepTimeout{
			StepName:            stepName,
			OriginalDirectiveID: originalID,
		},
	}
}

// recordSet is the assertion surface over the records plan emitted for one directive.
type recordSet struct {
	t       *testing.T
	records []model.Record
}

func newRecordSet(t *testing.T, records []model.Record) recordSet {
	t.Helper()
	return recordSet{t: t, records: records}
}

// historyKinds lists the history events in emission order — used for diagnostics on a miss.
func (rs recordSet) historyKinds() []ext.HistoryKind {
	kinds := make([]ext.HistoryKind, 0, len(rs.records))
	for _, r := range rs.records {
		if rec, ok := r.(model.HistoryRecord); ok {
			kinds = append(kinds, rec.History.Kind)
		}
	}
	return kinds
}

func (rs recordSet) requireHistory(kind ext.HistoryKind) ext.HistoryEvent {
	rs.t.Helper()
	for _, r := range rs.records {
		if rec, ok := r.(model.HistoryRecord); ok && rec.History.Kind == kind {
			return rec.History
		}
	}
	require.Failf(rs.t, "missing history event", "expected a %q history event, got %v", kind, rs.historyKinds())
	return ext.HistoryEvent{}
}

// finalRunState returns the last run-state transition emitted. A single directive may
// emit several (e.g. run start passes through start → step); the run ends in the last.
func (rs recordSet) finalRunState() ext.RunState {
	rs.t.Helper()
	var state ext.RunState
	found := false
	for _, r := range rs.records {
		if rec, ok := r.(model.RunStateRecord); ok {
			state = rec.State
			found = true
		}
	}
	require.True(rs.t, found, "expected a run-state transition but none was emitted")
	return state
}

func (rs recordSet) requireTimer(kind ext.TimerKind) ext.Timer {
	rs.t.Helper()
	for _, r := range rs.records {
		if rec, ok := r.(model.TimerRecord); ok && rec.Timer.Kind == kind {
			return rec.Timer
		}
	}
	require.Failf(rs.t, "missing timer", "expected a %q timer to be set", kind)
	return ext.Timer{}
}

// requireDispatchedStep asserts that stepName was handed to a worker for execution.
func (rs recordSet) requireDispatchedStep(stepName string) {
	rs.t.Helper()
	for _, r := range rs.records {
		rec, ok := r.(model.WorkerTaskDispatch)
		if !ok {
			continue
		}
		if msg, ok := rec.Directive.Msg.(ext.DispatchableMessage); ok && msg.StepName() == stepName {
			return
		}
	}
	require.Failf(rs.t, "missing dispatched step", "expected step %q to be dispatched to a worker", stepName)
}

func (rs recordSet) requireBgTask(kind ext.BackgroundTaskKind) ext.BackgroundTask {
	rs.t.Helper()
	for _, r := range rs.records {
		if rec, ok := r.(model.BackgroundTaskRecord); ok && rec.Task.Kind == kind {
			return rec.Task
		}
	}
	require.Failf(rs.t, "missing background task", "expected a %q background task", kind)
	return ext.BackgroundTask{}
}

// requireParentNotification returns the decoded payload of the single background task
// that delivers this run's terminal outcome to its parent's callback step.
func (rs recordSet) requireParentNotification() ext.NotifyParentCompletePayload {
	rs.t.Helper()
	task := rs.requireBgTask(ext.BackgroundTaskKindNotifyParentComplete)
	var payload ext.NotifyParentCompletePayload
	require.NoError(rs.t, msgpack.Unmarshal(task.Payload, &payload))
	return payload
}

func (rs recordSet) requireNoBgTask(kind ext.BackgroundTaskKind) {
	rs.t.Helper()
	for _, r := range rs.records {
		if rec, ok := r.(model.BackgroundTaskRecord); ok && rec.Task.Kind == kind {
			require.Failf(rs.t, "unexpected background task", "did not expect a %q background task", kind)
		}
	}
}

func (rs recordSet) requireRunOutput() any {
	rs.t.Helper()
	for _, r := range rs.records {
		if rec, ok := r.(model.RunOutputRecord); ok {
			return rec.Result
		}
	}
	require.Fail(rs.t, "expected a run-output record but none was emitted")
	return nil
}

func (rs recordSet) requireRunError() ext.ErrorDetails {
	rs.t.Helper()
	for _, r := range rs.records {
		if rec, ok := r.(model.RunErrorRecord); ok {
			return rec.Error
		}
	}
	require.Fail(rs.t, "expected a run-error record but none was emitted")
	return ext.ErrorDetails{}
}

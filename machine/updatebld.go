package machine

import (
	"errors"
	"fmt"
	"grctl/server/store"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"time"
)

var (
	ErrRunAlreadyCompleted = errors.New("run is already completed")
	ErrStaleDirective      = errors.New("stale step timeout directive")
)

type UpdateFactory struct{}

func newUpdateFactory() *UpdateFactory {
	return &UpdateFactory{}
}

func (f *UpdateFactory) BuildUpdates(sn store.StateSnapshot, d ext.Directive) ([]store.StateUpdate, error) {
	slog.Debug("Building updates for directive", "WFID", d.RunInfo.WFID, "RunID", d.RunInfo.ID, "DirectiveKind", d.Kind, "RunState", sn.RunState.Kind)

	var updates []store.StateUpdate
	var err error

	switch d.Kind {
	case ext.DirectiveKindStepResult:
		updates, err = f.updatesFromStepResult(sn, d)
	case ext.DirectiveKindEvent:
		updates, err = f.updatesFromEvent(sn, d)
	case ext.DirectiveKindWaitEvent:
		slog.Debug("Starting WaitEvent transition")
		updates, err = f.updatesFromWaitEvent(sn, d)
	default:
		updates, err = f.buildTransitionUpdates(d, sn.RunState)
	}

	if err != nil {
		if errors.Is(err, ErrStaleDirective) {
			return nil, ErrStaleDirective
		}
		slog.Error("failed to build state updates for directive", "error", err, "WFID", d.RunInfo.WFID, "RunID", d.RunInfo.ID, "DirectiveKind", d.Kind)
		return f.BuildUnexpectedFailure(d, err.Error(), sn.RunState)
	}

	return updates, nil
}

func (f *UpdateFactory) updatesFromWaitEvent(sn store.StateSnapshot, d ext.Directive) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 10)
	_, ok := d.Msg.(*ext.WaitEvent)
	if !ok {
		return nil, fmt.Errorf("expected WaitEvent message but got %T", d.Msg)
	}

	if sn.Event.ID != "" {
		// If we're transitioning into WaitEvent state but there's already an event in the snapshot,
		// we need to transition into the step for that event
		stepUpdates, err := f.StartStep(sn.Event, sn.RunState)
		if err != nil {
			return nil, fmt.Errorf("failed to build updates for WaitEvent directive while inbox has pending event: %w", err)
		}
		updates = append(updates, stepUpdates...)
	} else {
		waitEventUpdates, err := f.WaitEvent(d, sn.RunState)
		if err != nil {
			return nil, fmt.Errorf("failed to build updates for WaitEvent directive: %w", err)
		}
		updates = append(updates, waitEventUpdates...)
	}

	return updates, nil
}

func (f *UpdateFactory) updatesFromStepResult(sn store.StateSnapshot, d ext.Directive) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 10)
	msg, ok := d.Msg.(*ext.StepResult)
	if !ok {
		return nil, fmt.Errorf("expected StepResult message but got %T", d.Msg)
	}

	completeUpdates, err := f.CompleteStep(d, sn.RunState)
	if err != nil {
		return nil, fmt.Errorf("failed to build updates for completing step: %w", err)
	}

	nextD := ext.Directive{
		ID:        ext.DeriveNextDirectiveID(d.ID),
		Timestamp: time.Now().UTC(),
		Kind:      msg.NextMsgKind,
		RunInfo:   d.RunInfo,
		Msg:       msg.NextMsg,
	}

	nextDUpdates, err := f.buildNextDirectiveUpdates(sn, nextD)
	slog.Debug("Next directive from step result:", "kind", nextD.Kind)
	if err != nil {
		return nil, fmt.Errorf("failed to build updates for next directive after completing step: %w", err)
	}

	updates = append(updates, completeUpdates...)
	updates = append(updates, nextDUpdates...)

	return updates, nil

}

func (f *UpdateFactory) updatesFromEvent(sn store.StateSnapshot, d ext.Directive) ([]store.StateUpdate, error) {
	updates, err := f.EventReceived(d)
	if err != nil {
		return nil, fmt.Errorf("failed to build updates for event received in non WaitEvent state: %w", err)
	}

	// Always transition the current inbox head while waiting for events.
	// This preserves inbox order and keeps deleteFromInbox aligned with the processed directive.
	if sn.RunState.Kind == ext.RunStateWaitEvent && sn.Event.ID != "" {
		nextD := sn.Event
		stepUpdates, err := f.StartStep(nextD, sn.RunState)
		slog.Debug("Creating step updates for event received in WaitEvent state", "StepUpdates", stepUpdates)
		if err != nil {
			return nil, fmt.Errorf("failed to build updates for event received in WaitEvent state: %w", err)
		}
		updates = append(updates, stepUpdates...)

		// Schedule cleanup of the inbox event that was just dispatched.
		if event, ok := nextD.Msg.(*ext.Event); ok && event.EventSeqID != nil {
			inboxTask, err := ext.NewDeleteInboxEventTask(nextD.ID, *event.EventSeqID)
			if err != nil {
				return nil, fmt.Errorf("failed to create delete inbox event background task: %w", err)
			}
			updates = append(updates, store.BackgroundTaskUpdate{Task: inboxTask})
		}
	}

	return updates, nil
}

func (f *UpdateFactory) buildNextDirectiveUpdates(sn store.StateSnapshot, d ext.Directive) ([]store.StateUpdate, error) {
	switch d.Kind {
	case ext.DirectiveKindWaitEvent:
		return f.updatesFromWaitEvent(sn, d)
	default:
		return f.buildTransitionUpdates(d, sn.RunState)
	}
}

func (f *UpdateFactory) buildTransitionUpdates(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	slog.Debug("Building updates for directive", "WFID", d.RunInfo.WFID, "DirectiveKind", d.Kind)

	switch d.Kind {
	case ext.DirectiveKindStart:
		return f.StartRun(d)
	case ext.DirectiveKindStep, ext.DirectiveKindEvent:
		return f.StartStep(d, currentState)
	case ext.DirectiveKindStepTimeout:
		return f.BuildStepTimeout(d, currentState)
	case ext.DirectiveKindComplete:
		return f.CompleteRun(d, currentState)
	case ext.DirectiveKindFail:
		return f.FailRun(d, currentState)
	case ext.DirectiveKindCancel:
		return f.CancelRun(d, currentState)
	default:
		slog.Warn("unknown directive kind", "kind", d.Kind)
		// Unknown kinds should be ACKed to avoid retry loops
		return nil, fmt.Errorf("unknown directive kind: %s", d.Kind)
	}
}

func (f *UpdateFactory) StartRun(d ext.Directive) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 4)
	startTime := time.Now().UTC()

	h, err := NewHistoryBuilder().RunStarted(d, startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to create run started history event: %w", err)
	}
	historyAppend := store.HistoryUpdate{History: h}

	ri := d.RunInfo
	ri, err = ri.Start(startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to create run started state update: %w", err)
	}
	runInfoUpdate := store.RunInfoUpdate{Info: ri}

	runState := ext.NewRunState(d, ext.RunStateStart)
	runState.StartedAt = ri.StartedAt
	runStateUpdate := store.RunStateUpdate{
		State: runState,
	}

	// Update the directive's RunInfo so the timer created in StartStep
	// carries the correct StartedAt (needed for timeout failure paths).
	d.RunInfo = ri

	startStepUpdates, err := f.StartStep(d, runState)
	if err != nil {
		return nil, fmt.Errorf("failed to create start step updates during run start: %w", err)
	}

	updates = append(updates, historyAppend)
	updates = append(updates, runInfoUpdate)
	updates = append(updates, runStateUpdate)

	if start, ok := d.Msg.(*ext.Start); ok && start.Input != nil {
		updates = append(updates, store.RunInputUpdate{
			WFID:  d.RunInfo.WFID,
			RunID: d.RunInfo.ID,
			Input: start.Input,
		})
	}

	updates = append(updates, startStepUpdates...)

	slog.Debug("StartRun transition created", "WFID", d.RunInfo.WFID, "RunID", d.RunInfo.ID)

	return updates, nil
}

func (f *UpdateFactory) StartStep(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 4)

	d.RunInfo.HistorySeqID = currentState.SeqID

	msg, ok := d.Msg.(ext.DispatchableMessage)
	if !ok {
		return nil, fmt.Errorf("can not create step started state update: expected DispatchableMessage but got %T", d.Msg)
	}

	stepName := msg.StepName()

	timer, err := createStepTimeoutTimer(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create step timeout timer: %w", err)
	}

	timerCreate := store.TimerUpdate{
		Timer: timer,
	}

	ri := d.RunInfo
	if ri.StartedAt == nil && currentState.StartedAt != nil {
		ri.StartedAt = currentState.StartedAt
	}
	ri, err = ri.StartStep(stepName, d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create step started state update: %w", err)
	}

	runInfoUpdate := store.RunInfoUpdate{Info: ri}

	d.RunInfo = ri
	workerTaskDispatch := store.WorkerTaskDispatch{
		Directive: d,
	}

	runState := currentState
	runState.Kind = ext.RunStateStep
	runState.EnteredAt = time.Now().UTC()
	runState.ActiveDirectiveID = d.ID
	if d.Kind == ext.DirectiveKindEvent {
		if event, ok := d.Msg.(*ext.Event); ok && event.EventSeqID != nil {
			runState.LastEventSeqID = *event.EventSeqID
		}
	}

	runStateUpdate := store.RunStateUpdate{
		State:       runState,
		ExpectedSeq: currentState.SeqID,
	}

	updates = append(updates, timerCreate)
	updates = append(updates, runInfoUpdate)
	updates = append(updates, workerTaskDispatch)
	updates = append(updates, runStateUpdate)

	slog.Debug("StartStep transition created", "WFID", d.RunInfo.WFID, "RunID", d.RunInfo.ID, "StepName", stepName, "TimerID", timer.ID)

	return updates, nil

}

func (f *UpdateFactory) CompleteStep(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	msg, ok := d.Msg.(*ext.StepResult)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create step completed state update: expected StepResult but got %T", d.Msg)
	}

	updates := make([]store.StateUpdate, 0, 3)
	ri := d.RunInfo

	if msg.KVUpdates != nil {
		kvUpdate := store.KVUpdate{
			WFID:    ri.WFID,
			RunID:   ri.ID,
			Updates: *msg.KVUpdates,
		}
		updates = append(updates, kvUpdate)
	}

	h, err := NewHistoryBuilder().StepCompleted(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create step completed history event: %w", err)
	}
	updates = append(updates, store.HistoryUpdate{History: h})

	timerID := ext.DeriveTimerID(currentState.ActiveDirectiveID, ext.TimerKindStepTimeout)
	timerTask, err := ext.NewDeleteTimerTask(d.ID, ri.WFID, ext.TimerKindStepTimeout, timerID)
	if err != nil {
		return nil, fmt.Errorf("failed to create delete timer background task: %w", err)
	}
	updates = append(updates, store.BackgroundTaskUpdate{Task: timerTask})

	return updates, nil
}

func (f *UpdateFactory) WaitEvent(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 3)
	msg, ok := d.Msg.(*ext.WaitEvent)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create wait event state update: expected WaitEvent but got %T", d.Msg)
	}

	ri := d.RunInfo
	ri, err := ri.WaitEvent(time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to create wait event state update: %w", err)
	}
	updates = append(updates, store.RunInfoUpdate{Info: ri})

	h, err := NewHistoryBuilder().WaitEventStarted(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create wait event history event: %w", err)
	}
	updates = append(updates, store.HistoryUpdate{History: h})

	runState := currentState
	runState.Kind = ext.RunStateWaitEvent
	runState.EnteredAt = time.Now().UTC()
	runState.ActiveDirectiveID = ""

	updates = append(updates, store.RunStateUpdate{
		State:       runState,
		ExpectedSeq: currentState.SeqID,
	})

	return updates, nil
}

func (f *UpdateFactory) EventReceived(d ext.Directive) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 1)
	msg, ok := d.Msg.(*ext.Event)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create event received state update: expected Event but got %T", d.Msg)
	}

	inboxUpdate := store.InboxUpdate{
		Directive: d,
	}
	updates = append(updates, inboxUpdate)

	h, err := NewHistoryBuilder().EventReceived(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create event received history event: %w", err)
	}
	updates = append(updates, store.HistoryUpdate{History: h})

	return updates, nil
}

func (f *UpdateFactory) CompleteRun(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 5)
	ri := d.RunInfo
	ri, err := ri.Complete(d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create run completed state update: %w", err)
	}
	updates = append(updates, store.RunInfoUpdate{Info: ri})

	h, err := NewHistoryBuilder().RunCompleted(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run completed history event: %w", err)
	}
	updates = append(updates, store.HistoryUpdate{History: h})

	state := currentState
	state.Kind = ext.RunStateComplete
	state.EnteredAt = time.Now().UTC()
	state.ActiveDirectiveID = ""

	updates = append(updates, store.RunStateUpdate{
		State:       state,
		ExpectedSeq: currentState.SeqID,
	})

	if msg, ok := d.Msg.(*ext.Complete); ok && msg.Result != nil {
		updates = append(updates, store.RunOutputUpdate{
			WFID:   d.RunInfo.WFID,
			RunID:  d.RunInfo.ID,
			Result: msg.Result,
		})
	}

	purgeTask, err := ext.NewPurgeRunResidueTask(d.ID, d.RunInfo.WFID)
	if err != nil {
		return nil, fmt.Errorf("build purge run residue task: %w", err)
	}
	updates = append(updates, store.BackgroundTaskUpdate{Task: purgeTask})

	return updates, nil
}

func (f *UpdateFactory) CancelReceived(d ext.Directive) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 2)
	updates = append(updates, store.InboxUpdate{Directive: d})

	he, err := NewHistoryBuilder().RunCancelScheduled(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run cancel scheduled history event: %w", err)
	}
	updates = append(updates, store.HistoryUpdate{History: he})

	return updates, nil
}

func (f *UpdateFactory) CancelRun(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 4)
	ri := d.RunInfo
	ri, err := ri.Cancel(d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create run cancelled state update: %w", err)
	}
	updates = append(updates, store.RunInfoUpdate{Info: ri})

	h, err := NewHistoryBuilder().RunCancelled(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run cancelled history event: %w", err)
	}
	updates = append(updates, store.HistoryUpdate{History: h})

	state := currentState
	state.Kind = ext.RunStateCancel
	state.EnteredAt = time.Now().UTC()
	state.ActiveDirectiveID = ""

	updates = append(updates, store.RunStateUpdate{
		State:       state,
		ExpectedSeq: currentState.SeqID,
	})

	purgeTask, err := ext.NewPurgeRunResidueTask(d.ID, d.RunInfo.WFID)
	if err != nil {
		return nil, fmt.Errorf("build purge run residue task: %w", err)
	}
	updates = append(updates, store.BackgroundTaskUpdate{Task: purgeTask})

	return updates, nil
}

func (f *UpdateFactory) FailRun(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	updates := make([]store.StateUpdate, 0, 5)
	msg, ok := d.Msg.(*ext.Fail)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create run failed state update: expected RunFailed but got %T", d.Msg)
	}

	ri := d.RunInfo
	ri, err := ri.Fail(d.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create run failed state update: %w", err)
	}
	updates = append(updates, store.RunInfoUpdate{Info: ri})

	he, err := NewHistoryBuilder().RunFailed(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create run completed history event: %w", err)
	}
	updates = append(updates, store.HistoryUpdate{History: he})

	state := currentState
	state.Kind = ext.RunStateFail
	state.EnteredAt = time.Now().UTC()
	state.ActiveDirectiveID = ""

	updates = append(updates, store.RunStateUpdate{
		State:       state,
		ExpectedSeq: currentState.SeqID,
	})

	updates = append(updates, store.RunErrorUpdate{
		WFID:  d.RunInfo.WFID,
		RunID: d.RunInfo.ID,
		Error: msg.Error,
	})

	purgeTask, err := ext.NewPurgeRunResidueTask(d.ID, d.RunInfo.WFID)
	if err != nil {
		return nil, fmt.Errorf("build purge run residue task: %w", err)
	}
	updates = append(updates, store.BackgroundTaskUpdate{Task: purgeTask})

	return updates, nil
}

func (f *UpdateFactory) BuildStepTimeout(d ext.Directive, currentState ext.RunState) ([]store.StateUpdate, error) {
	msg, ok := d.Msg.(*ext.StepTimeout)
	if !ok {
		return nil, fmt.Errorf("can not create step timeout updates: expected StepTimeout but got %T", d.Msg)
	}

	if currentState.Kind != ext.RunStateStep || currentState.ActiveDirectiveID != msg.OriginalDirectiveID {
		return nil, ErrStaleDirective
	}

	he, err := NewHistoryBuilder().StepTimedout(d, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create step timeout history event: %w", err)
	}

	updates := make([]store.StateUpdate, 0, 5)
	updates = append(updates, store.HistoryUpdate{History: he})

	failUpdates, err := f.StepTimeoutFailure(d, msg.StepName, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create step timeout failure updates: %w", err)
	}
	updates = append(updates, failUpdates...)

	return updates, nil
}

// Receives any directive and turns it into a Fail directive with contextual cause.
func (f *UpdateFactory) BuildUnexpectedFailure(d ext.Directive, cause string, currentState ext.RunState) ([]store.StateUpdate, error) {
	failDirective := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindFail,
		RunInfo:   d.RunInfo,
		Msg: &ext.Fail{
			Error: ext.ErrorDetails{
				Type:    "UnexpectedFailure",
				Message: fmt.Sprintf("An unexpected error occurred while processing directive %s of kind %s: %s", d.ID, d.Kind, cause),
			},
		},
	}

	return f.FailRun(failDirective, currentState)
}

func (f *UpdateFactory) StepTimeoutFailure(d ext.Directive, stepName string, currentState ext.RunState) ([]store.StateUpdate, error) {
	failDirective := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindFail,
		RunInfo:   d.RunInfo,
		Msg: &ext.Fail{
			Error: ext.ErrorDetails{
				Type:    "StepTimeout",
				Message: fmt.Sprintf("step %s timed out", stepName),
			},
		},
	}

	return f.FailRun(failDirective, currentState)
}

func createStepTimeoutTimer(d ext.Directive) (ext.Timer, error) {
	const defaultStepTimeoutMS = 3 * 1000

	msg, ok := d.Msg.(ext.DispatchableMessage)
	if !ok {
		return ext.Timer{}, fmt.Errorf("unexpected message type for directive kind %s: got %T", d.Kind, msg)
	}

	stepName := msg.StepName()
	timeout := msg.TimeoutMS()
	currentTime := time.Now().UTC()
	if timeout == 0 {
		timeout = defaultStepTimeoutMS
	}
	expiresAt := currentTime.Add(time.Duration(timeout) * time.Millisecond)

	timer := ext.Timer{
		ID:        ext.DeriveTimerID(d.ID, ext.TimerKindStepTimeout),
		Kind:      ext.TimerKindStepTimeout,
		WFID:      d.RunInfo.WFID,
		ExpiresAt: expiresAt,
		CreatedAt: currentTime,
		StepName:  stepName,
		Directive: d,
	}

	return timer, nil
}

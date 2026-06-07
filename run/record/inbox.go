package record

import (
	"fmt"
	"grctl/server/run/history"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"time"
)

func Wait(d ext.Directive, sn model.StateSnapshot, defaultWaitTimeoutMS uint32) ([]model.Record, error) {
	records := make([]model.Record, 0, 5)
	msg, ok := d.Msg.(*ext.Wait)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create wait state update: expected Wait but got %T", d.Msg)
	}
	slog.Debug("Wait directive received", "timeout_ms", msg.Timeout, "timeout_step_name", msg.TimeoutStepName)

	currentState := sn.RunState

	ri := d.RunInfo
	ri, err := ri.Wait(time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to create wait state update: %w", err)
	}
	records = append(records, model.RunInfoRecord{Info: ri})

	h, err := history.WaitStarted(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create wait started history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: h})

	runState := currentState
	runState.Kind = ext.RunStateWait
	runState.EnteredAt = time.Now().UTC()
	runState.ActiveDirectiveID = ""

	timeout := msg.Timeout
	if timeout == 0 {
		timeout = defaultWaitTimeoutMS
	}
	timer, err := createWaitTimeoutTimer(d, timeout, msg.TimeoutStepName)
	if err != nil {
		return nil, fmt.Errorf("failed to create wait timeout timer: %w", err)
	}
	records = append(records, model.TimerRecord{Timer: timer})
	runState.ActiveDirectiveID = d.ID

	records = append(records, model.RunStateRecord{
		State:       runState,
		ExpectedSeq: currentState.SeqID,
	})

	// If an inbox event arrived before the run reached Wait, publish a WakeFromInbox
	// directive so the manager processes it in a separate batch — avoiding a second
	// RunStateRecord in this batch which would conflict on the same OCC sequence.
	if sn.Event.ID != "" {
		wakeD := ext.Directive{
			ID:        ext.DeriveNextDirectiveID(d.ID),
			Timestamp: time.Now().UTC(),
			Kind:      ext.DirectiveKindWakeFromInbox,
			RunInfo:   d.RunInfo,
			Msg:       &ext.WakeFromInbox{},
		}
		records = append(records, model.DirectiveRecord{Directive: wakeD})
	}

	return records, nil
}

func EventReceived(d ext.Directive, sn model.StateSnapshot) ([]model.Record, error) {
	records := make([]model.Record, 0, 3)
	msg, ok := d.Msg.(*ext.Event)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create event received state update: expected Event but got %T", d.Msg)
	}

	records = append(records, model.InboxRecord{Directive: d})

	h, err := history.EventReceived(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create event received history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: h})

	// If the run is already in Wait with an empty inbox, publish WakeFromInbox so
	// the manager re-evaluates immediately — by then the event is in the snapshot
	// with its JetStream sequence ID assigned.
	if sn.RunState.Kind == ext.RunStateWait && sn.Event.ID == "" {
		wakeD := ext.Directive{
			ID:        ext.DeriveNextDirectiveID(d.ID),
			Timestamp: time.Now().UTC(),
			Kind:      ext.DirectiveKindWakeFromInbox,
			RunInfo:   d.RunInfo,
			Msg:       &ext.WakeFromInbox{},
		}
		records = append(records, model.DirectiveRecord{Directive: wakeD})
	}

	return records, nil
}

// Creates the records for an even directive.
// A bit of duplication with StepStart, but separated for clarity.
// We should merge it when we have clear RunInfo update logic.
func EventStart(d ext.Directive, currentState ext.RunState, defaultTimeoutMS uint32) ([]model.Record, error) {
	records := make([]model.Record, 0, 4)

	d.RunInfo.HistorySeqID = currentState.SeqID

	msg, ok := d.Msg.(*ext.Event)
	if !ok {
		return nil, fmt.Errorf("can not create step started state update: expected DispatchableMessage but got %T", d.Msg)
	}

	stepName := msg.StepName()

	timer, err := StepTimeoutTimer(d, defaultTimeoutMS)
	if err != nil {
		return nil, fmt.Errorf("failed to create step timeout timer: %w", err)
	}

	timerCreate := model.TimerRecord{
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

	runInfoUpdate := model.RunInfoRecord{Info: ri}

	d.RunInfo = ri
	workerTaskDispatch := model.WorkerTaskDispatch{
		Directive: d,
	}

	runState := currentState
	runState.Kind = ext.RunStateStep
	runState.EnteredAt = time.Now().UTC()
	runState.ActiveDirectiveID = d.ID

	// the EventSeqID is only assigned when an event is read back from JetStream, not when it arrives as an incoming directive.
	//
	if msg.EventSeqID != nil {
		runState.LastEventSeqID = *msg.EventSeqID
	}

	runStateUpdate := model.RunStateRecord{
		State:       runState,
		ExpectedSeq: currentState.SeqID,
	}

	records = append(records, timerCreate)
	records = append(records, runInfoUpdate)
	records = append(records, workerTaskDispatch)
	records = append(records, runStateUpdate)

	slog.Debug("startStep transition created")

	return records, nil

}

func WaitTimeout(d ext.Directive, currentState ext.RunState, defaultTimeoutMS uint32) ([]model.Record, error) {
	slog.Debug("WaitTimeout triggered")
	msg, ok := d.Msg.(*ext.WaitTimeout)
	if !ok {
		return nil, fmt.Errorf("can not create wait timeout records: expected WaitTimeout but got %T", d.Msg)
	}

	if currentState.Kind != ext.RunStateWait || currentState.ActiveDirectiveID != msg.OriginalDirectiveID {
		return nil, ErrStaleDirective
	}

	records := make([]model.Record, 0, 4)

	he, err := history.WaitTimedOut(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create wait timed out history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: he})

	if msg.TimeoutStepName != "" {
		stepD := ext.Directive{
			ID:        ext.DeriveNextDirectiveID(d.ID),
			Timestamp: time.Now().UTC(),
			Kind:      ext.DirectiveKindStep,
			RunInfo:   d.RunInfo,
			Msg:       &ext.Step{Name: msg.TimeoutStepName},
		}
		steprecords, err := StepStart(stepD, currentState, defaultTimeoutMS)
		if err != nil {
			return nil, fmt.Errorf("failed to start timeout step %q: %w", msg.TimeoutStepName, err)
		}
		records = append(records, steprecords...)
		return records, nil
	}

	failDirective := ext.Directive{
		ID:        ext.NewDirectiveID(),
		Timestamp: time.Now().UTC(),
		Kind:      ext.DirectiveKindFail,
		RunInfo:   d.RunInfo,
		Msg: &ext.Fail{
			Error: ext.ErrorDetails{
				Type:    "WaitTimeout",
				Message: "wait timed out without a timeout step",
			},
		},
	}
	failRecords, err := FailRun(failDirective, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to fail run on wait timeout: %w", err)
	}
	records = append(records, failRecords...)

	return records, nil
}

func createWaitTimeoutTimer(d ext.Directive, timeout uint32, timeoutStepName string) (ext.Timer, error) {
	now := time.Now().UTC()
	return ext.Timer{
		ID:        ext.DeriveTimerID(d.ID, ext.TimerKindWaitTimeout),
		Kind:      ext.TimerKindWaitTimeout,
		WFID:      d.RunInfo.WFID,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(timeout) * time.Millisecond),
		StepName:  timeoutStepName,
		Directive: d,
	}, nil
}

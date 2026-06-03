package history

import (
	"fmt"
	ext "grctl/server/types/external/v1"
	"time"
)

func RunStarted(d ext.Directive, timestamp time.Time) (ext.HistoryEvent, error) {
	s, ok := d.Msg.(*ext.Start)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected Start directive but got %T", d.Msg)
	}

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: timestamp,
		Kind:      ext.HistoryKindRunStarted,
		Msg: ext.RunStarted{
			Input: s.Input,
		},
	}

	return e, nil
}

func RunCompleted(d ext.Directive) (ext.HistoryEvent, error) {
	dmsg, ok := d.Msg.(*ext.Complete)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected Complete directive but got %T", d.Msg)
	}
	durationMS := d.Timestamp.Sub(*d.RunInfo.StartedAt).Milliseconds()

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: d.Timestamp,
		Kind:      ext.HistoryKindRunCompleted,
		Msg: ext.RunCompleted{
			Result:     dmsg.Result,
			DurationMS: durationMS,
		},
	}

	return e, nil
}

func RunFailed(d ext.Directive) (ext.HistoryEvent, error) {
	dmsg, ok := d.Msg.(*ext.Fail)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected Complete directive but got %T", d.Msg)
	}

	durationMS := d.Timestamp.Sub(*d.RunInfo.StartedAt).Milliseconds()

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: d.Timestamp,
		Kind:      ext.HistoryKindRunFailed,
		Msg: ext.RunFailed{
			Error:      dmsg.Error,
			DurationMS: durationMS,
		},
	}

	return e, nil
}

func RunCancelScheduled(d ext.Directive) (ext.HistoryEvent, error) {
	_, ok := d.Msg.(*ext.Cancel)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected CancelSchedule directive but got %T", d.Msg)
	}

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: d.Timestamp,
		Kind:      ext.HistoryKindRunCancelScheduled,
		Msg:       ext.RunCancelScheduled{},
	}

	return e, nil
}

func RunCancelReceived(d ext.Directive) (ext.HistoryEvent, error) {
	_, ok := d.Msg.(*ext.Cancel)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected CancelSchedule directive but got %T", d.Msg)
	}

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: d.Timestamp,
		Kind:      ext.HistoryKindRunCancelReceived,
		Msg:       ext.RunCancelReceived{},
	}

	return e, nil
}

func RunTerminated(d ext.Directive) (ext.HistoryEvent, error) {
	dmsg, ok := d.Msg.(*ext.Terminate)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected Terminate directive but got %T", d.Msg)
	}

	var durationMS int64
	if d.RunInfo.StartedAt != nil {
		durationMS = d.Timestamp.Sub(*d.RunInfo.StartedAt).Milliseconds()
	}

	return ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: d.Timestamp,
		Kind:      ext.HistoryKindRunTerminated,
		Msg: ext.RunTerminated{
			Reason:     dmsg.Reason,
			DurationMS: durationMS,
		},
	}, nil
}

func RunCancelled(d ext.Directive) (ext.HistoryEvent, error) {
	dmsg, ok := d.Msg.(*ext.Cancel)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected Cancel directive but got %T", d.Msg)
	}

	durationMS := d.Timestamp.Sub(*d.RunInfo.StartedAt).Milliseconds()

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: d.Timestamp,
		Kind:      ext.HistoryKindRunCancelled,
		Msg: ext.RunCancelled{
			Reason:     dmsg.Reason,
			DurationMS: durationMS,
		},
	}

	return e, nil
}

func StepStarted(d ext.Directive) (ext.HistoryEvent, error) {
	var stepName string
	if d.Kind == ext.DirectiveKindStart {
		stepName = "start"
	} else {
		msg, ok := d.Msg.(*ext.Step)
		if !ok {
			return ext.HistoryEvent{}, fmt.Errorf("expected StepResult directive but got %T", d.Msg)
		}
		stepName = msg.Name
	}

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindStepStarted,
		Msg: ext.StepStarted{
			StepName: stepName,
		},
	}

	return e, nil
}

func StepPickedUp(d ext.Directive) (ext.HistoryEvent, error) {
	msg, ok := d.Msg.(*ext.StepPickedUp)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("expected StepPickedUp directive but got %T", d.Msg)
	}

	return ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		WorkerID:  msg.WorkerID,
		Timestamp: msg.Timestamp,
		Kind:      ext.HistoryKindStepStarted,
		Msg: &ext.StepStarted{
			StepName: msg.StepName,
			WorkerID: msg.WorkerID,
		},
	}, nil
}

func StepCompleted(msg ext.StepResult, ri ext.RunInfo) (ext.HistoryEvent, error) {
	stepName, err := msg.ProcStepName()
	if err != nil {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish step complete event: %w", err)
	}

	workerID := ext.WorkerID("")
	if msg.WorkerID != nil {
		workerID = *msg.WorkerID
	}

	e := ext.HistoryEvent{
		WFID:      ri.WFID,
		RunID:     ri.ID,
		WorkerID:  workerID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindStepCompleted,
		Msg: ext.StepCompleted{
			StepName:   stepName,
			WorkerID:   workerID,
			DurationMS: msg.DurationMS,
		},
	}

	return e, nil
}

func StepFailed(msg ext.StepResult, ri ext.RunInfo) (ext.HistoryEvent, error) {
	stepName, err := msg.ProcStepName()
	if err != nil {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish step failed event: %w", err)
	}
	failMsg, ok := msg.NextMsg.(*ext.FailStep)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish step failed event: expected FailStep next msg but got %T", msg.NextMsg)
	}

	e := ext.HistoryEvent{
		WFID:      ri.WFID,
		RunID:     ri.ID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindStepFailed,
		Msg: ext.StepFailed{
			StepName:   stepName,
			Error:      failMsg.Error,
			DurationMS: msg.DurationMS,
		},
	}

	return e, nil
}

func StepTimedout(d ext.Directive, currentState ext.RunState) (ext.HistoryEvent, error) {
	msg, ok := d.Msg.(*ext.StepTimeout)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish step timeout event: expected StepTimeout but got %T", d.Msg)
	}

	stepName := msg.StepName

	durationMS := time.Since(currentState.EnteredAt).Milliseconds()

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindStepTimeout,
		Msg: ext.StepTimedout{
			StepName:   stepName,
			DurationMS: durationMS,
		},
	}

	return e, nil
}

func WaitTimedOut(d ext.Directive) (ext.HistoryEvent, error) {
	msg, ok := d.Msg.(*ext.WaitTimeout)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish wait timed out history event: expected WaitTimeout directive but got %T", d.Msg)
	}

	return ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindWaitTimedOut,
		Msg:       ext.WaitTimedOut{OriginalDirectiveID: msg.OriginalDirectiveID, TimeoutStepName: msg.TimeoutStepName},
	}, nil
}

func WaitStarted(d ext.Directive) (ext.HistoryEvent, error) {
	msg, ok := d.Msg.(*ext.Wait)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish wait started history event: expected Wait directive but got %T", d.Msg)
	}

	return ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindWaitStarted,
		Msg:       ext.WaitStarted{DirectiveID: d.ID, Timeout: msg.Timeout, TimeoutStepName: msg.TimeoutStepName},
	}, nil
}

func EventReceived(d ext.Directive) (ext.HistoryEvent, error) {
	msg, ok := d.Msg.(*ext.Event)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish event received history event: expected Event directive but got %T", d.Msg)
	}

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindEventReceived,
		Msg: ext.EventReceived{
			EventName: msg.EventName,
			Payload:   msg.Payload,
		},
	}

	return e, nil
}

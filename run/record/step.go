package record

import (
	"fmt"
	"grctl/server/run/history"
	model "grctl/server/types"
	ext "grctl/server/types/external/v1"
	"log/slog"
	"time"
)

func StepStart(d ext.Directive, currentState ext.RunState, defaultTimeoutMS uint32) ([]model.Record, error) {
	records := make([]model.Record, 0, 4)

	d.RunInfo.HistorySeqID = currentState.SeqID
	if d.RunInfo.StartedAt == nil && currentState.StartedAt != nil {
		d.RunInfo.StartedAt = currentState.StartedAt
	}

	msg, ok := d.Msg.(ext.DispatchableMessage)
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
	if d.Kind == ext.DirectiveKindEvent {
		if event, ok := d.Msg.(*ext.Event); ok && event.EventSeqID != nil {
			runState.LastEventSeqID = *event.EventSeqID
		}
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

func StepPickedUp(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	msg, ok := d.Msg.(*ext.StepPickedUp)
	if !ok || msg == nil {
		return nil, fmt.Errorf("expected StepPickedUp but got %T", d.Msg)
	}

	h, err := history.StepPickedUp(d)
	if err != nil {
		return nil, fmt.Errorf("failed to create step.started history event: %w", err)
	}

	runState := currentState
	runState.WorkerID = &msg.WorkerID

	return []model.Record{
		model.HistoryRecord{History: h},
		model.RunStateRecord{
			State:       runState,
			ExpectedSeq: currentState.SeqID,
		},
	}, nil
}

func StepComplete(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	msg, ok := d.Msg.(*ext.StepResult)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create step completed state update: expected StepResult but got %T", d.Msg)
	}

	records := make([]model.Record, 0, 3)
	ri := d.RunInfo

	if msg.KVUpdates != nil {
		kvUpdate := model.KVRecord{
			WFID:  ri.WFID,
			RunID: ri.ID,
			KVMap: *msg.KVUpdates,
		}
		records = append(records, kvUpdate)
	}

	h, err := history.StepCompleted(*msg, ri)
	if err != nil {
		return nil, fmt.Errorf("failed to create step completed history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: h})

	deleteStepTimeoutTimerRecords, err := DeleteStepTimeoutTimer(d, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create step timeout timer updates: %w", err)
	}
	records = append(records, deleteStepTimeoutTimerRecords...)

	return records, nil
}

func StepFail(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	records := make([]model.Record, 0, 8)
	msg, ok := d.Msg.(*ext.StepResult)
	if !ok || msg == nil {
		return nil, fmt.Errorf("can not create step failed state update: expected StepResult but got %T", d.Msg)
	}
	failMsg, ok := msg.NextMsg.(*ext.FailStep)
	if !ok || failMsg == nil {
		return nil, fmt.Errorf("can not create step failed state update: expected FailStep next msg but got %T", msg.NextMsg)
	}

	if msg.KVUpdates != nil {
		records = append(records, model.KVRecord{
			WFID:  d.RunInfo.WFID,
			RunID: d.RunInfo.ID,
			KVMap: *msg.KVUpdates,
		})
	}

	stepFailedEvent, err := history.StepFailed(*msg, d.RunInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create step failed history event: %w", err)
	}
	records = append(records, model.HistoryRecord{History: stepFailedEvent})

	deleteStepTimeoutTimerRecords, err := DeleteStepTimeoutTimer(d, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create delete step timeout timer updates: %w", err)
	}
	records = append(records, deleteStepTimeoutTimerRecords...)

	return records, nil
}

func StepTimeout(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	msg, ok := d.Msg.(*ext.StepTimeout)
	if !ok {
		return nil, fmt.Errorf("can not create step timeout records: expected StepTimeout but got %T", d.Msg)
	}

	if currentState.Kind != ext.RunStateStep || currentState.ActiveDirectiveID != msg.OriginalDirectiveID {
		return nil, ErrStaleDirective
	}

	he, err := history.StepTimedout(d, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create step timeout history event: %w", err)
	}

	updates := make([]model.Record, 0, 5)
	updates = append(updates, model.HistoryRecord{History: he})

	failUpdates, err := StepTimeoutFailure(d, msg.StepName, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create step timeout failure updates: %w", err)
	}
	updates = append(updates, failUpdates...)

	return updates, nil
}

func StepTimeoutTimer(d ext.Directive, defaultTimeoutMS uint32) (ext.Timer, error) {
	msg, ok := d.Msg.(ext.DispatchableMessage)
	if !ok {
		return ext.Timer{}, fmt.Errorf("unexpected message type for directive kind %s: got %T", d.Kind, msg)
	}

	stepName := msg.StepName()
	timeout := msg.TimeoutMS()
	currentTime := time.Now().UTC()
	if timeout == 0 {
		timeout = defaultTimeoutMS
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

func StepTimeoutFailure(d ext.Directive, stepName string, currentState ext.RunState) ([]model.Record, error) {
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

	return FailRun(failDirective, currentState)
}

func DeleteStepTimeoutTimer(d ext.Directive, currentState ext.RunState) ([]model.Record, error) {
	ri := d.RunInfo
	timerID := ext.DeriveTimerID(currentState.ActiveDirectiveID, ext.TimerKindStepTimeout)
	timerTask, err := ext.NewDeleteTimerTask(d.ID, ri.WFID, ext.TimerKindStepTimeout, timerID)
	if err != nil {
		return nil, fmt.Errorf("failed to create delete timer background task: %w", err)
	}
	updates := []model.Record{model.BackgroundTaskRecord{Task: timerTask}}
	return updates, nil
}

package machine

import (
	"fmt"
	ext "grctl/server/types/external/v1"
	"time"
)

type HistoryBuilder struct{}

func NewHistoryBuilder() *HistoryBuilder {
	return &HistoryBuilder{}
}

func (b *HistoryBuilder) RunStarted(d ext.Directive, timestamp time.Time) (ext.HistoryEvent, error) {
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

func (b *HistoryBuilder) RunCompleted(d ext.Directive) (ext.HistoryEvent, error) {
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

func (b *HistoryBuilder) RunFailed(d ext.Directive) (ext.HistoryEvent, error) {
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

func (b *HistoryBuilder) RunCancelScheduled(d ext.Directive) (ext.HistoryEvent, error) {
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

func (b *HistoryBuilder) RunCancelReceived(d ext.Directive) (ext.HistoryEvent, error) {
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

func (b *HistoryBuilder) RunCancelled(d ext.Directive) (ext.HistoryEvent, error) {
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

func (b *HistoryBuilder) StepStarted(d ext.Directive) (ext.HistoryEvent, error) {
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

func (p *HistoryBuilder) StepCompleted(d ext.Directive) (ext.HistoryEvent, error) {
	msg, ok := d.Msg.(*ext.StepResult)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish step complete event: expected StepResult but got %T", d.Msg)
	}
	stepName, err := msg.ProcStepName()
	if err != nil {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish step complete event: %w", err)
	}

	workerID := ext.WorkerID("")
	if msg.WorkerID != nil {
		workerID = *msg.WorkerID
	}

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
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

func (p *HistoryBuilder) StepTimedout(d ext.Directive, currentState ext.RunState) (ext.HistoryEvent, error) {
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

func (p *HistoryBuilder) WaitEventStarted(d ext.Directive) (ext.HistoryEvent, error) {
	_, ok := d.Msg.(*ext.WaitEvent)
	if !ok {
		return ext.HistoryEvent{}, fmt.Errorf("cannot publish wait event started history event: expected WaitEvent directive but got %T", d.Msg)
	}

	e := ext.HistoryEvent{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Timestamp: time.Now().UTC(),
		Kind:      ext.HistoryKindWaitEventStarted,
		Msg:       ext.WaitEventStarted{},
	}

	return e, nil
}

func (p *HistoryBuilder) EventReceived(d ext.Directive) (ext.HistoryEvent, error) {
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

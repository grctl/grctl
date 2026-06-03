package external

import (
	"time"
)

type RunStateKind string

const (
	RunStateIdle      RunStateKind = "idle"
	RunStateStart     RunStateKind = "start"
	RunStateStep      RunStateKind = "step"
	RunStateWait      RunStateKind = "wait"
	RunStateComplete  RunStateKind = "complete"
	RunStateFail      RunStateKind = "fail"
	RunStateCancel    RunStateKind = "cancel"
	RunStateTerminate RunStateKind = "terminate"
)

type RunState struct {
	WFID           WFID         `json:"wf_id" msgpack:"wf_id"`
	RunID          RunID        `json:"run_id" msgpack:"run_id"`
	Kind           RunStateKind `json:"kind" msgpack:"kind"`
	EnteredAt      time.Time    `json:"entered_at" msgpack:"entered_at"`
	StartedAt      *time.Time   `json:"started_at,omitempty" msgpack:"started_at,omitempty"`
	LastEventSeqID uint64       `json:"last_event_seq_id" msgpack:"last_event_seq_id"`
	// ActiveDirectiveID is the ID of the directive that caused the current RunStateStep or RunStateWait.
	// Non-empty when Kind == RunStateStep, or Kind == RunStateWait with a timeout configured.
	// Used to detect stale timeout directives.
	ActiveDirectiveID DirectiveID `json:"active_directive_id,omitempty" msgpack:"active_directive_id,omitempty"`
	// WorkerID is the ID of the worker currently executing the active step (set when
	// the worker publishes StepPickedUp). Nil until the first step is picked up.
	WorkerID *WorkerID `json:"worker_id,omitempty" msgpack:"worker_id,omitempty"`

	// PendingCancel holds the cancel directive received while a step is in progress.
	// planFromStepResult checks this after the step finishes and transitions to cancelled.
	PendingCancel *Directive `json:"pending_cancel,omitempty" msgpack:"pending_cancel,omitempty"`

	// SeqID is the NATS stream sequence of the entry. Populated on read, not serialized.
	SeqID uint64 `json:"-" msgpack:"-"`
}

func (s RunState) IsTerminal() bool {
	return s.Kind == RunStateComplete || s.Kind == RunStateFail || s.Kind == RunStateCancel || s.Kind == RunStateTerminate
}

func NewRunState(d Directive, kind RunStateKind) RunState {
	return RunState{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Kind:      kind,
		EnteredAt: time.Now().UTC(),
	}
}

package external

import (
	"time"
)

type RunStateKind string

const (
	RunStateIdle      RunStateKind = "idle"
	RunStateStart     RunStateKind = "start"
	RunStateStep      RunStateKind = "step"
	RunStateSleep     RunStateKind = "sleep"
	RunStateSleepUntl RunStateKind = "sleep_until"
	RunStateWaitEvent RunStateKind = "wait_event"
	RunStateComplete  RunStateKind = "complete"
	RunStateFail      RunStateKind = "fail"
	RunStateCancel    RunStateKind = "cancel"
)

type RunState struct {
	WFID           WFID         `json:"wf_id" msgpack:"wf_id"`
	RunID          RunID        `json:"run_id" msgpack:"run_id"`
	Kind           RunStateKind `json:"kind" msgpack:"kind"`
	EnteredAt      time.Time    `json:"entered_at" msgpack:"entered_at"`
	StartedAt      *time.Time   `json:"started_at,omitempty" msgpack:"started_at,omitempty"`
	LastEventSeqID uint64       `json:"last_event_seq_id" msgpack:"last_event_seq_id"`
	// ActiveDirectiveID is the ID of the directive that caused the current RunStateStep.
	// Non-empty only when Kind == RunStateStep. Used to detect stale step timeout directives.
	ActiveDirectiveID DirectiveID `json:"active_directive_id,omitempty" msgpack:"active_directive_id,omitempty"`

	// SeqID is the NATS stream sequence of the entry. Populated on read, not serialized.
	SeqID uint64 `json:"-" msgpack:"-"`
}

func (s RunState) IsTerminal() bool {
	return s.Kind == RunStateComplete || s.Kind == RunStateFail || s.Kind == RunStateCancel
}

func NewRunState(d Directive, kind RunStateKind) RunState {
	return RunState{
		WFID:      d.RunInfo.WFID,
		RunID:     d.RunInfo.ID,
		Kind:      kind,
		EnteredAt: time.Now().UTC(),
	}
}

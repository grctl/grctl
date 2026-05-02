package external

import (
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusScheduled RunStatus = "scheduled"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
	RunStatusTimedOut  RunStatus = "timed_out"
)

func (s RunStatus) IsTerminal() bool {
	return s == RunStatusCompleted ||
		s == RunStatusFailed ||
		s == RunStatusCancelled ||
		s == RunStatusTimedOut
}

type WFID string

func NewWFID() WFID {
	return WFID(ulid.Make().String())
}

func (wf WFID) String() string {
	return string(wf)
}

type RunID string

func NewRunID() RunID {
	return RunID(ulid.Make().String())
}

func (run RunID) String() string {
	return string(run)
}

type WFType string

func NewWFType(name string) WFType {
	return WFType(name)
}

type RunInfo struct {
	ID           RunID          `json:"id" msgpack:"id"` //run_id
	WFID         WFID           `json:"wf_id" msgpack:"wf_id"`
	WFType       WFType         `json:"wf_type" msgpack:"wf_type"`
	Status       RunStatus      `json:"status,omitempty" msgpack:"status,omitempty"`
	WorkerName   WorkerName     `json:"worker_name,omitempty" msgpack:"worker_name,omitempty"`
	ParentWFID   *WFID          `json:"parent_wf_id,omitempty" msgpack:"parent_wf_id,omitempty"`
	ParentWFType *WFType        `json:"parent_wf_type,omitempty" msgpack:"parent_wf_type,omitempty"`
	ParentRunID  *RunID         `json:"parent_run_id,omitempty" msgpack:"parent_run_id,omitempty"`
	Timeout      *time.Duration `json:"timeout,omitempty" msgpack:"timeout,omitempty"`
	CreatedAt    time.Time      `json:"created_at" msgpack:"created_at"`
	ScheduledAt  *time.Time     `json:"scheduled_at,omitempty" msgpack:"scheduled_at,omitempty"`
	StartedAt    *time.Time     `json:"started_at,omitempty" msgpack:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty" msgpack:"completed_at,omitempty"`
	// HistorySeqID is the NATS stream sequence of the RunState message from the previous
	// transition. Since RunStateUpdate is always the last message in each batch, all history
	// entries from prior transitions have sequences <= this value. Workers use this as an
	// upper bound when loading history for replay. Zero means no prior history exists.
	HistorySeqID uint64 `json:"history_seq_id,omitempty" msgpack:"history_seq_id,omitempty"`
}

func (ri RunInfo) IsTerminal() bool {
	return ri.Status.IsTerminal()
}

func (ri RunInfo) Pending(timestamp time.Time) (RunInfo, error) {
	if ri.CreatedAt.IsZero() {
		ri.CreatedAt = timestamp
	}

	ri.Status = RunStatusPending
	return ri, nil
}

func (ri RunInfo) Schedule(timestamp time.Time) (RunInfo, error) {
	if ri.CreatedAt.IsZero() {
		ri.CreatedAt = timestamp
	}

	ri.Status = RunStatusScheduled
	ri.ScheduledAt = &timestamp
	return ri, nil
}

func (ri RunInfo) Start(timestamp time.Time) (RunInfo, error) {
	if ri.CreatedAt.IsZero() {
		ri.CreatedAt = timestamp
	}

	ri.Status = RunStatusRunning
	ri.StartedAt = &timestamp
	return ri, nil
}

func (ri RunInfo) StartStep(stepName string, timestamp time.Time) (RunInfo, error) {
	if ri.CreatedAt.IsZero() {
		ri.CreatedAt = timestamp
	}

	ri.Status = RunStatusRunning
	return ri, nil
}

func (ri RunInfo) WaitEvent(timestamp time.Time) (RunInfo, error) {
	ri.Status = RunStatusRunning
	return ri, nil
}

func (ri RunInfo) StartEvent(eventName string, timestamp time.Time) (RunInfo, error) {
	if ri.CreatedAt.IsZero() {
		ri.CreatedAt = timestamp
	}

	ri.Status = RunStatusRunning
	return ri, nil
}

func (ri RunInfo) Complete(timestamp time.Time) (RunInfo, error) {
	if ri.StartedAt == nil {
		return RunInfo{}, fmt.Errorf("cannot complete run without start time")
	}

	ri.Status = RunStatusCompleted
	ri.CompletedAt = &timestamp
	return ri, nil
}

func (ri RunInfo) Fail(timestamp time.Time) (RunInfo, error) {
	if ri.StartedAt == nil {
		return RunInfo{}, fmt.Errorf("cannot complete run without start time")
	}
	ri.Status = RunStatusFailed
	ri.CompletedAt = &timestamp
	return ri, nil
}

func (ri RunInfo) Cancel(timestamp time.Time) (RunInfo, error) {
	ri.Status = RunStatusCancelled
	ri.CompletedAt = &timestamp
	return ri, nil
}

func (ri RunInfo) MarkTimedOut(timestamp time.Time) (RunInfo, error) {
	if ri.StartedAt == nil {
		return RunInfo{}, fmt.Errorf("cannot complete run without start time")
	}

	ri.Status = RunStatusTimedOut
	ri.CompletedAt = &timestamp
	return ri, nil
}

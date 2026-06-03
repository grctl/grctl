package external

import (
	"fmt"

	"github.com/vmihailenco/msgpack/v5"
)

type BackgroundTaskKind string

const (
	BackgroundTaskKindDeleteTimer          BackgroundTaskKind = "delete_timer"
	BackgroundTaskKindDeleteInboxEvent     BackgroundTaskKind = "delete_inbox_event"
	BackgroundTaskKindPurgeRunResidue      BackgroundTaskKind = "purge_run_residue"
	BackgroundTaskKindNotifyParentComplete BackgroundTaskKind = "notify_parent_complete"
	BackgroundTaskKindWorkerTerminateRun   BackgroundTaskKind = "worker_terminate_run"
)

type BackgroundTask struct {
	Kind BackgroundTaskKind `msgpack:"kind"`
	// This can be turned to BGTaskID. Since all bg tasks may not need deduplication,
	DeduplicationID DirectiveID `msgpack:"deduplication_id"`
	Payload         []byte      `msgpack:"payload"`
}

// DeleteTimerPayload is the msgpack-encoded Payload for BackgroundTaskKindDeleteTimer.
type DeleteTimerPayload struct {
	WFID    WFID      `msgpack:"wf_id"`
	Kind    TimerKind `msgpack:"kind"`
	TimerID TimerID   `msgpack:"timer_id"`
}

// DeleteInboxEventPayload is the msgpack-encoded Payload for BackgroundTaskKindDeleteInboxEvent.
type DeleteInboxEventPayload struct {
	SeqID uint64 `msgpack:"seq_id"`
}

// PurgeRunResiduePayload is the msgpack-encoded Payload for BackgroundTaskKindPurgeRunResidue.
type PurgeRunResiduePayload struct {
	WFID WFID `msgpack:"wf_id"`
}

// NotifyParentCompletePayload is the msgpack-encoded Payload for
// BackgroundTaskKindNotifyParentComplete. It carries everything the handler needs
// to deliver the child's terminal outcome to the parent's callback step without a
// second store lookup of the child run. Result is set when Status is completed;
// Error is set for failed/cancelled/timed-out outcomes.
type NotifyParentCompletePayload struct {
	ParentWFID WFID          `msgpack:"parent_wf_id"`
	StepName   string        `msgpack:"step_name"`
	ChildWFID  WFID          `msgpack:"child_wf_id"`
	Status     RunStatus     `msgpack:"status"`
	Result     any           `msgpack:"result,omitempty"`
	Error      *ErrorDetails `msgpack:"error,omitempty"`
}

// DeriveBgTaskID produces a deterministic DirectiveID for a background task derived from
// the originating directive. This ensures dedup across redeliveries.
func DeriveBgTaskID(parentID DirectiveID, taskKind BackgroundTaskKind) DirectiveID {
	return DirectiveID(fmt.Sprintf("bg.%s.%s", taskKind, parentID))
}

// NewDeleteTimerTask constructs a BackgroundTask that deletes a specific timer by WFID, kind, and timer ID.
func NewDeleteTimerTask(parentID DirectiveID, wfID WFID, kind TimerKind, timerID TimerID) (BackgroundTask, error) {
	payload, err := msgpack.Marshal(&DeleteTimerPayload{WFID: wfID, Kind: kind, TimerID: timerID})
	if err != nil {
		return BackgroundTask{}, fmt.Errorf("failed to marshal delete timer payload: %w", err)
	}
	return BackgroundTask{
		Kind:            BackgroundTaskKindDeleteTimer,
		DeduplicationID: DeriveBgTaskID(parentID, BackgroundTaskKindDeleteTimer),
		Payload:         payload,
	}, nil
}

// NewDeleteInboxEventTask constructs a BackgroundTask that deletes an inbox event by stream sequence.
func NewDeleteInboxEventTask(parentID DirectiveID, seqID uint64) (BackgroundTask, error) {
	payload, err := msgpack.Marshal(&DeleteInboxEventPayload{SeqID: seqID})
	if err != nil {
		return BackgroundTask{}, fmt.Errorf("failed to marshal delete inbox event payload: %w", err)
	}
	return BackgroundTask{
		Kind:            BackgroundTaskKindDeleteInboxEvent,
		DeduplicationID: DeriveBgTaskID(parentID, BackgroundTaskKindDeleteInboxEvent),
		Payload:         payload,
	}, nil
}

// NewPurgeRunResidueTask constructs a BackgroundTask that purges all wfID-scoped residue
// (directives, timers, cancel inbox, event inbox, worker tasks) for a completed/failed/cancelled run.
func NewPurgeRunResidueTask(parentID DirectiveID, wfID WFID) (BackgroundTask, error) {
	payload, err := msgpack.Marshal(&PurgeRunResiduePayload{WFID: wfID})
	if err != nil {
		return BackgroundTask{}, fmt.Errorf("failed to marshal purge run residue payload: %w", err)
	}
	return BackgroundTask{
		Kind:            BackgroundTaskKindPurgeRunResidue,
		DeduplicationID: DeriveBgTaskID(parentID, BackgroundTaskKindPurgeRunResidue),
		Payload:         payload,
	}, nil
}

// WorkerTerminateRunPayload is the msgpack-encoded Payload for BackgroundTaskKindWorkerTerminateRun.
type WorkerTerminateRunPayload struct {
	WorkerID WorkerID `msgpack:"worker_id"`
	RunID    RunID    `msgpack:"run_id"`
}

// NewWorkerTerminateRunTask constructs a BackgroundTask that sends a terminate command to a
// specific worker to abort an in-flight step. The command is best-effort: if the worker
// is already gone, the handler logs and discards the task.
func NewWorkerTerminateRunTask(parentID DirectiveID, workerID WorkerID, runID RunID) (BackgroundTask, error) {
	payload, err := msgpack.Marshal(&WorkerTerminateRunPayload{WorkerID: workerID, RunID: runID})
	if err != nil {
		return BackgroundTask{}, fmt.Errorf("failed to marshal worker terminate run payload: %w", err)
	}
	return BackgroundTask{
		Kind:            BackgroundTaskKindWorkerTerminateRun,
		DeduplicationID: DeriveBgTaskID(parentID, BackgroundTaskKindWorkerTerminateRun),
		Payload:         payload,
	}, nil
}

// NewNotifyParentCompleteTask constructs a BackgroundTask that delivers a child's terminal
// outcome to its parent's callback step. parentID is the child's terminal directive ID, so
// the derived dedup ID makes redelivery of the same terminal transition notify the parent once.
func NewNotifyParentCompleteTask(parentID DirectiveID, payload NotifyParentCompletePayload) (BackgroundTask, error) {
	encoded, err := msgpack.Marshal(&payload)
	if err != nil {
		return BackgroundTask{}, fmt.Errorf("failed to marshal notify parent complete payload: %w", err)
	}
	return BackgroundTask{
		Kind:            BackgroundTaskKindNotifyParentComplete,
		DeduplicationID: DeriveBgTaskID(parentID, BackgroundTaskKindNotifyParentComplete),
		Payload:         encoded,
	}, nil
}

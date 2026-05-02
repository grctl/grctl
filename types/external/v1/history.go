package external

import (
	"fmt"
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

type HistoryKind string

const HistoryKindRunScheduled HistoryKind = "run.scheduled"
const HistoryKindRunStarted HistoryKind = "run.started"
const HistoryKindRunTimeout HistoryKind = "run.timeout"
const HistoryKindRunCancelReceived HistoryKind = "run.cancel_received"
const HistoryKindRunCancelScheduled HistoryKind = "run.cancel_scheduled"
const HistoryKindRunCancelled HistoryKind = "run.cancelled"
const HistoryKindRunCompleted HistoryKind = "run.completed"
const HistoryKindRunFailed HistoryKind = "run.failed"

const HistoryKindStepCancelled HistoryKind = "step.cancelled"
const HistoryKindStepCompleted HistoryKind = "step.completed"
const HistoryKindStepFailed HistoryKind = "step.failed"
const HistoryKindStepStarted HistoryKind = "step.started"
const HistoryKindStepTimeout HistoryKind = "step.timeout"

const HistoryKindWaitEventStarted HistoryKind = "wait_event.started"
const HistoryKindEventReceived HistoryKind = "event.received"

const HistoryKindTaskCancelled HistoryKind = "task.cancelled"
const HistoryKindTaskCompleted HistoryKind = "task.completed"
const HistoryKindTaskFailed HistoryKind = "task.failed"
const HistoryKindTaskAttemptFailed HistoryKind = "task.attempt_failed"
const HistoryKindTaskStarted HistoryKind = "task.started"

const HistoryKindTimestampRecorded HistoryKind = "timestamp.recorded"
const HistoryKindRandomRecorded HistoryKind = "random.recorded"
const HistoryKindUUIDRecorded HistoryKind = "uuid.recorded"
const HistoryKindSleepRecorded HistoryKind = "sleep.recorded"
const HistoryKindChildStarted HistoryKind = "child.started"
const HistoryKindParentEventSent HistoryKind = "parent.event_sent"

// HistoryEvent represents an envelope for history messages.
type HistoryEvent struct {
	WFID      WFID           `json:"wf_id" msgpack:"wf_id"`
	RunID     RunID          `json:"run_id" msgpack:"run_id"`
	WorkerID  WorkerID       `json:"worker_id" msgpack:"worker_id"`
	Timestamp time.Time      `json:"timestamp" msgpack:"timestamp"`
	Kind      HistoryKind    `json:"kind" msgpack:"kind"`
	Msg       HistoryMessage `json:"msg" msgpack:"msg"`
}

// HistoryMessage encapsulates event specific payloads.
type HistoryMessage interface {
	isHistoryMessage()
}

// RunScheduledMessage represents a queued workflow run.
type RunScheduled struct{}

// RunStarted represents the beginning of a workflow execution.
type RunStarted struct {
	Input any `json:"input" msgpack:"input"`
}

// RunCompleted represents a successful workflow completion.
type RunCompleted struct {
	Result     any   `json:"result" msgpack:"result"`
	DurationMS int64 `json:"duration_ms" msgpack:"duration_ms"`
}

// RunFailed represents a workflow failure.
type RunFailed struct {
	Error      ErrorDetails `json:"error" msgpack:"error"`
	DurationMS int64        `json:"duration_ms" msgpack:"duration_ms"`
}

type RunCancelScheduled struct{}
type RunCancelReceived struct{}

// RunCancelled represents a cancelled workflow.
type RunCancelled struct {
	Reason     string `json:"reason" msgpack:"reason"`
	DurationMS int64  `json:"duration_ms" msgpack:"duration_ms"`
}

// RunTimeout represents a workflow timeout.
type RunTimeout struct {
	DurationMS int64 `json:"duration_ms" msgpack:"duration_ms"`
}

// StepStarted represents the beginning of a step.
type StepStarted struct {
	StepName string `json:"step_name" msgpack:"step_name"`
}

// StepCompleted represents a successful step completion.
type StepCompleted struct {
	StepName   string   `json:"step_name" msgpack:"step_name"`
	WorkerID   WorkerID `json:"worker_id" msgpack:"worker_id"`
	DurationMS int64    `json:"duration_ms" msgpack:"duration_ms"`
}

// StepFailed represents a failed step.
type StepFailed struct {
	StepName   string       `json:"step_name" msgpack:"step_name"`
	Error      ErrorDetails `json:"error" msgpack:"error"`
	DurationMS int64        `json:"duration_ms" msgpack:"duration_ms"`
}

// StepCancelled represents a cancelled step.
type StepCancelled struct {
	StepName string `json:"step_name" msgpack:"step_name"`
}

// StepTimeout represents a step timeout.
type StepTimedout struct {
	StepName   string `json:"step_name" msgpack:"step_name"`
	DurationMS int64  `json:"duration_ms" msgpack:"duration_ms"`
}

type WaitEventStarted struct{}

type EventReceived struct {
	EventName string `json:"event_name" msgpack:"event_name"`
	Payload   any    `json:"payload" msgpack:"payload"`
}

// TaskStarted represents the beginning of a task within a step.
type TaskStarted struct {
	TaskID   string `json:"task_id" msgpack:"task_id"`
	TaskName string `json:"task_name" msgpack:"task_name"`
	Args     any    `json:"args" msgpack:"args"`
	StepName string `json:"step_name" msgpack:"step_name"`
}

// TaskCompleted represents a successful task completion.
type TaskCompleted struct {
	TaskID     string `json:"task_id" msgpack:"task_id"`
	TaskName   string `json:"task_name" msgpack:"task_name"`
	Output     any    `json:"output" msgpack:"output"`
	StepName   string `json:"step_name" msgpack:"step_name"`
	DurationMS int64  `json:"duration_ms" msgpack:"duration_ms"`
}

// TaskFailed represents a failed task.
type TaskFailed struct {
	TaskID     string       `json:"task_id" msgpack:"task_id"`
	TaskName   string       `json:"task_name" msgpack:"task_name"`
	StepName   string       `json:"step_name" msgpack:"step_name"`
	Error      ErrorDetails `json:"error" msgpack:"error"`
	DurationMS int64        `json:"duration_ms" msgpack:"duration_ms"`
}

// TaskCancelled represents a cancelled task.
type TaskCancelled struct {
	TaskID     string `json:"task_id" msgpack:"task_id"`
	TaskName   string `json:"task_name" msgpack:"task_name"`
	StepName   string `json:"step_name" msgpack:"step_name"`
	DurationMS int64  `json:"duration_ms" msgpack:"duration_ms"`
}

// TaskAttemptFailed represents a failed task retry attempt (task will be retried).
type TaskAttemptFailed struct {
	TaskID           string       `json:"task_id" msgpack:"task_id"`
	TaskName         string       `json:"task_name" msgpack:"task_name"`
	StepName         string       `json:"step_name" msgpack:"step_name"`
	Attempt          int          `json:"attempt" msgpack:"attempt"`
	MaxAttempts      int          `json:"max_attempts" msgpack:"max_attempts"`
	Error            ErrorDetails `json:"error" msgpack:"error"`
	NextRetryDelayMS int64        `json:"next_retry_delay_ms" msgpack:"next_retry_delay_ms"`
	DurationMS       int64        `json:"duration_ms" msgpack:"duration_ms"`
}

type TimestampRecorded struct {
	Value time.Time `json:"value" msgpack:"value"`
}

type RandomRecorded struct {
	Value float64 `json:"value" msgpack:"value"`
}

type UUIDRecorded struct {
	Value string `json:"value" msgpack:"value"`
}

type SleepRecorded struct {
	DurationMS int64 `json:"duration_ms" msgpack:"duration_ms"`
}

type ChildWorkflowStarted struct {
	RunID  RunID  `json:"run_id" msgpack:"run_id"`
	WFType string `json:"wf_type" msgpack:"wf_type"`
	WFID   WFID   `json:"wf_id" msgpack:"wf_id"`
	Input  any    `json:"input,omitempty" msgpack:"input,omitempty"`
}

type ParentEventSent struct {
	EventName    string `json:"event_name" msgpack:"event_name"`
	Payload      any    `json:"payload" msgpack:"payload"`
	ParentWFType string `json:"parent_wf_type,omitempty" msgpack:"parent_wf_type,omitempty"`
	ParentWFID   string `json:"parent_wf_id,omitempty" msgpack:"parent_wf_id,omitempty"`
}

func (RunScheduled) isHistoryMessage()       {}
func (RunStarted) isHistoryMessage()         {}
func (RunCompleted) isHistoryMessage()       {}
func (RunFailed) isHistoryMessage()          {}
func (RunCancelScheduled) isHistoryMessage() {}
func (RunCancelReceived) isHistoryMessage()  {}
func (RunCancelled) isHistoryMessage()       {}
func (RunTimeout) isHistoryMessage()         {}

func (StepStarted) isHistoryMessage()   {}
func (StepCompleted) isHistoryMessage() {}
func (StepFailed) isHistoryMessage()    {}
func (StepCancelled) isHistoryMessage() {}
func (StepTimedout) isHistoryMessage()  {}

func (WaitEventStarted) isHistoryMessage() {}
func (EventReceived) isHistoryMessage()    {}

func (TaskStarted) isHistoryMessage()       {}
func (TaskCompleted) isHistoryMessage()     {}
func (TaskFailed) isHistoryMessage()        {}
func (TaskAttemptFailed) isHistoryMessage() {}
func (TaskCancelled) isHistoryMessage()     {}

func (TimestampRecorded) isHistoryMessage()    {}
func (RandomRecorded) isHistoryMessage()       {}
func (UUIDRecorded) isHistoryMessage()         {}
func (SleepRecorded) isHistoryMessage()        {}
func (ChildWorkflowStarted) isHistoryMessage() {}
func (ParentEventSent) isHistoryMessage()      {}

var historyMessageFactories = map[HistoryKind]func() HistoryMessage{
	HistoryKindRunScheduled:       func() HistoryMessage { return &RunScheduled{} },
	HistoryKindRunStarted:         func() HistoryMessage { return &RunStarted{} },
	HistoryKindRunCompleted:       func() HistoryMessage { return &RunCompleted{} },
	HistoryKindRunFailed:          func() HistoryMessage { return &RunFailed{} },
	HistoryKindRunCancelScheduled: func() HistoryMessage { return &RunCancelScheduled{} },
	HistoryKindRunCancelReceived:  func() HistoryMessage { return &RunCancelReceived{} },
	HistoryKindRunCancelled:       func() HistoryMessage { return &RunCancelled{} },
	HistoryKindRunTimeout:         func() HistoryMessage { return &RunTimeout{} },

	HistoryKindStepStarted:   func() HistoryMessage { return &StepStarted{} },
	HistoryKindStepCompleted: func() HistoryMessage { return &StepCompleted{} },
	HistoryKindStepFailed:    func() HistoryMessage { return &StepFailed{} },
	HistoryKindStepCancelled: func() HistoryMessage { return &StepCancelled{} },
	HistoryKindStepTimeout:   func() HistoryMessage { return &StepTimedout{} },

	HistoryKindWaitEventStarted: func() HistoryMessage { return &WaitEventStarted{} },
	HistoryKindEventReceived:    func() HistoryMessage { return &EventReceived{} },

	HistoryKindTaskStarted:       func() HistoryMessage { return &TaskStarted{} },
	HistoryKindTaskCompleted:     func() HistoryMessage { return &TaskCompleted{} },
	HistoryKindTaskFailed:        func() HistoryMessage { return &TaskFailed{} },
	HistoryKindTaskAttemptFailed: func() HistoryMessage { return &TaskAttemptFailed{} },
	HistoryKindTaskCancelled:     func() HistoryMessage { return &TaskCancelled{} },

	HistoryKindTimestampRecorded: func() HistoryMessage { return &TimestampRecorded{} },
	HistoryKindRandomRecorded:    func() HistoryMessage { return &RandomRecorded{} },
	HistoryKindUUIDRecorded:      func() HistoryMessage { return &UUIDRecorded{} },
	HistoryKindSleepRecorded:     func() HistoryMessage { return &SleepRecorded{} },
	HistoryKindChildStarted:      func() HistoryMessage { return &ChildWorkflowStarted{} },
	HistoryKindParentEventSent:   func() HistoryMessage { return &ParentEventSent{} },
}

type historyWire struct {
	WFID      WFID        `msgpack:"w"`
	RunID     RunID       `msgpack:"r"`
	WorkerID  WorkerID    `msgpack:"wo"`
	Timestamp time.Time   `msgpack:"ts"`
	Kind      HistoryKind `msgpack:"k"`
	Msg       []byte      `msgpack:"m"`
}

// EncodeMsgpack implements custom msgpack encoding for HistoryEvent
func (e *HistoryEvent) EncodeMsgpack(enc *msgpack.Encoder) error {
	if e.Msg == nil {
		return fmt.Errorf("history message cannot be nil")
	}

	msgBytes, err := msgpack.Marshal(e.Msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	wire := historyWire{
		WFID:      e.WFID,
		RunID:     e.RunID,
		WorkerID:  e.WorkerID,
		Timestamp: e.Timestamp,
		Kind:      e.Kind,
		Msg:       msgBytes,
	}

	return enc.Encode(&wire)
}

// DecodeMsgpack implements custom msgpack decoding for HistoryEvent
func (e *HistoryEvent) DecodeMsgpack(dec *msgpack.Decoder) error {
	// Decode all fields
	var wire historyWire
	if err := dec.Decode(&wire); err != nil {
		return fmt.Errorf("failed to decode history: %w", err)
	}

	factory, ok := historyMessageFactories[wire.Kind]
	if !ok {
		return fmt.Errorf("unknown history kind: %s", wire.Kind)
	}

	msg := factory()
	if err := msgpack.Unmarshal(wire.Msg, msg); err != nil {
		return fmt.Errorf("failed to unmarshal message for kind %s: %w", wire.Kind, err)
	}

	*e = HistoryEvent{
		WFID:      wire.WFID,
		RunID:     wire.RunID,
		WorkerID:  wire.WorkerID,
		Timestamp: wire.Timestamp,
		Kind:      wire.Kind,
		Msg:       msg,
	}

	return nil
}

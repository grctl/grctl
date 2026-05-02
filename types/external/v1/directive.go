package external

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/vmihailenco/msgpack/v5"
)

type DirectiveKind string
type DirectiveID string
type KVUpdates map[string]any
type KVRevs map[string]uint64

func NewDirectiveID() DirectiveID {
	return DirectiveID(ulid.Make().String())
}

// DeriveNextDirectiveID derives a deterministic ID for the next directive from a parent directive ID.
// This ensures that if the parent directive is re-delivered, the derived next directive ID is the same,
// enabling JetStream deduplication via Nats-Msg-Id.
func DeriveNextDirectiveID(parentID DirectiveID) DirectiveID {
	return DirectiveID("next." + string(parentID))
}

// Client -> Server
const DirectiveKindTerminate DirectiveKind = "terminate"
const DirectiveKindCancel DirectiveKind = "cancel"

// Client -> Server -> Worker
const DirectiveKindStart DirectiveKind = "start"
const DirectiveKindEvent DirectiveKind = "event"

// Worker -> Server
const DirectiveKindStepResult DirectiveKind = "step_result" //Successful step completion.
const DirectiveKindFail DirectiveKind = "fail"
const DirectiveKindComplete DirectiveKind = "complete"

// Server -> Worker -> Server
const DirectiveKindWaitEvent DirectiveKind = "wait_event"
const DirectiveKindSleep DirectiveKind = "sleep"
const DirectiveKindSleepUntil DirectiveKind = "sleep_until"
const DirectiveKindStep DirectiveKind = "step"

// Timeout directives
const DirectiveKindStepTimeout DirectiveKind = "step_timeout"

type DispatchableMessage interface {
	StepName() string
	TimeoutMS() uint32
}

type DirectiveMessage interface {
	isDirectiveMessage()
}

// Start directive sent by the worker to begin workflow execution
type Start struct {
	Input   any    `json:"input" msgpack:"input"`
	Timeout uint32 `json:"timeout_ms" msgpack:"timeout_ms"`
}

func (s Start) StepName() string {
	return "start"
}

func (s Start) TimeoutMS() uint32 {
	return s.Timeout
}

type Step struct {
	Name    string `json:"step_name" msgpack:"step_name"`
	Timeout uint32 `json:"timeout_ms" msgpack:"timeout_ms"`
}

func (s Step) StepName() string {
	return s.Name
}

func (s Step) TimeoutMS() uint32 {
	return s.Timeout
}

type StepTimeout struct {
	StepName            string      `json:"step_name" msgpack:"step_name"`
	OriginalDirectiveID DirectiveID `json:"original_directive_id,omitempty" msgpack:"original_directive_id,omitempty"`
}

type WaitEvent struct {
	Timeout         uint32 `json:"timeout_ms" msgpack:"timeout_ms"`
	TimeoutStepName string `json:"timeout_step_name" msgpack:"timeout_step_name"`
}

type Sleep struct {
	Duration     uint32 `json:"duration" msgpack:"duration_ms"`
	NextStepName string `json:"next_step_name" msgpack:"next_step_name"`
}

type SleepUntil struct {
	Until        time.Time `json:"until" msgpack:"until"`
	NextStepName string    `json:"next_step_name" msgpack:"next_step_name"`
}

type Cancel struct {
	Reason string `json:"reason" msgpack:"reason"`
}

type Terminate struct {
	Reason string `json:"reason" msgpack:"reason"`
}

type Event struct {
	EventSeqID      *uint64 `json:"event_seq_id,omitempty" msgpack:"event_seq_id,omitempty"`
	EventName       string  `json:"event_name" msgpack:"event_name"`
	Payload         any     `json:"payload" msgpack:"payload"`
	Timeout         uint32  `json:"timeout_ms" msgpack:"timeout_ms"`
	TimeoutStepName string  `json:"timeout_step_name" msgpack:"timeout_step_name"`
}

func (e Event) StepName() string {
	return e.EventName
}

func (e Event) TimeoutMS() uint32 {
	return e.Timeout
}

// Directives sent by the worker
type Complete struct {
	Result any `json:"result" msgpack:"result"`
}

type Fail struct {
	Error ErrorDetails `json:"error" msgpack:"error"`
}

type StepResult struct {
	ProcessedMsgKind DirectiveKind    `json:"processed_msg_kind" msgpack:"processed_msg_kind"`
	ProcessedMsg     DirectiveMessage `json:"processed_msg" msgpack:"processed_msg"`
	WorkerID         *WorkerID        `json:"worker_id" msgpack:"worker_id"`
	DurationMS       int64            `json:"duration_ms" msgpack:"duration_ms"`
	// Key-value updates sent by the worker to be applied.
	KVUpdates   *KVUpdates       `json:"kv_updates,omitempty" msgpack:"kv_updates,omitempty"`
	NextMsgKind DirectiveKind    `json:"next_msg_kind" msgpack:"next_msg_kind"`
	NextMsg     DirectiveMessage `json:"next_msg" msgpack:"next_msg"`
}

func (sr StepResult) ProcStepName() (string, error) {
	msg, ok := sr.ProcessedMsg.(DispatchableMessage)
	if !ok || msg == nil {
		return "", fmt.Errorf("wrong message type. Processed message should be DispatchableMessage: %T", sr.ProcessedMsg)
	}
	return msg.StepName(), nil
}

// stepResultWire is the internal wire format that encodes DirectiveMessage fields as raw bytes,
// matching the approach used by directiveWire for Directive.Msg.
type stepResultWire struct {
	ProcessedMsgKind DirectiveKind      `msgpack:"processed_msg_kind"`
	ProcessedMsg     msgpack.RawMessage `msgpack:"processed_msg"`
	WorkerID         *WorkerID          `msgpack:"worker_id"`
	DurationMS       int64              `msgpack:"duration_ms"`
	KVUpdates        *KVUpdates         `msgpack:"kv_updates,omitempty"`
	NextMsgKind      DirectiveKind      `msgpack:"next_msg_kind"`
	NextMsg          msgpack.RawMessage `msgpack:"next_msg"`
}

func (sr *StepResult) EncodeMsgpack(enc *msgpack.Encoder) error {
	pmBytes, err := msgpack.Marshal(sr.ProcessedMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal ProcessedMsg: %w", err)
	}

	nmBytes, err := msgpack.Marshal(sr.NextMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal NextMsg: %w", err)
	}

	wire := stepResultWire{
		ProcessedMsgKind: sr.ProcessedMsgKind,
		ProcessedMsg:     pmBytes,
		WorkerID:         sr.WorkerID,
		DurationMS:       sr.DurationMS,
		KVUpdates:        sr.KVUpdates,
		NextMsgKind:      sr.NextMsgKind,
		NextMsg:          nmBytes,
	}

	return enc.Encode(&wire)
}

func (sr *StepResult) DecodeMsgpack(dec *msgpack.Decoder) error {
	var wire stepResultWire
	if err := dec.Decode(&wire); err != nil {
		return fmt.Errorf("failed to decode step result: %w", err)
	}

	pmFactory, ok := DirectiveFactories[wire.ProcessedMsgKind]
	if !ok {
		return fmt.Errorf("unknown processed msg kind: %s", wire.ProcessedMsgKind)
	}
	processedMsg := pmFactory()
	if err := msgpack.Unmarshal(wire.ProcessedMsg, processedMsg); err != nil {
		return fmt.Errorf("failed to unmarshal ProcessedMsg: %w", err)
	}

	nmFactory, ok := DirectiveFactories[wire.NextMsgKind]
	if !ok {
		return fmt.Errorf("unknown next msg kind: %s", wire.NextMsgKind)
	}
	nextMsg := nmFactory()
	if err := msgpack.Unmarshal(wire.NextMsg, nextMsg); err != nil {
		return fmt.Errorf("failed to unmarshal NextMsg: %w", err)
	}

	*sr = StepResult{
		ProcessedMsgKind: wire.ProcessedMsgKind,
		ProcessedMsg:     processedMsg,
		WorkerID:         wire.WorkerID,
		DurationMS:       wire.DurationMS,
		KVUpdates:        wire.KVUpdates,
		NextMsgKind:      wire.NextMsgKind,
		NextMsg:          nextMsg,
	}

	return nil
}

func (Start) isDirectiveMessage()       {}
func (Cancel) isDirectiveMessage()      {}
func (Terminate) isDirectiveMessage()   {}
func (Event) isDirectiveMessage()       {}
func (Complete) isDirectiveMessage()    {}
func (Fail) isDirectiveMessage()        {}
func (Step) isDirectiveMessage()        {}
func (WaitEvent) isDirectiveMessage()   {}
func (Sleep) isDirectiveMessage()       {}
func (SleepUntil) isDirectiveMessage()  {}
func (StepResult) isDirectiveMessage()  {}
func (StepTimeout) isDirectiveMessage() {}

type Directive struct {
	ID        DirectiveID      `json:"id" msgpack:"id"`
	Timestamp time.Time        `json:"timestamp" msgpack:"timestamp"`
	Kind      DirectiveKind    `json:"kind" msgpack:"kind"`
	RunInfo   RunInfo          `json:"run_info" msgpack:"run_info"`
	Attempt   StepAttempt      `json:"attempt" msgpack:"attempt"`
	Msg       DirectiveMessage `json:"msg" msgpack:"msg"`
}

var DirectiveFactories = map[DirectiveKind]func() DirectiveMessage{
	DirectiveKindStart:       func() DirectiveMessage { return &Start{} },
	DirectiveKindCancel:      func() DirectiveMessage { return &Cancel{} },
	DirectiveKindTerminate:   func() DirectiveMessage { return &Terminate{} },
	DirectiveKindEvent:       func() DirectiveMessage { return &Event{} },
	DirectiveKindComplete:    func() DirectiveMessage { return &Complete{} },
	DirectiveKindFail:        func() DirectiveMessage { return &Fail{} },
	DirectiveKindStep:        func() DirectiveMessage { return &Step{} },
	DirectiveKindWaitEvent:   func() DirectiveMessage { return &WaitEvent{} },
	DirectiveKindSleep:       func() DirectiveMessage { return &Sleep{} },
	DirectiveKindSleepUntil:  func() DirectiveMessage { return &SleepUntil{} },
	DirectiveKindStepResult:  func() DirectiveMessage { return &StepResult{} },
	DirectiveKindStepTimeout: func() DirectiveMessage { return &StepTimeout{} },
}

// directiveWire is the internal wire format with short field names for compact encoding
type directiveWire struct {
	ID        DirectiveID   `msgpack:"id"`
	Kind      DirectiveKind `msgpack:"k"`
	Timestamp time.Time     `msgpack:"t"`
	RunInfo   RunInfo       `msgpack:"r"`
	Attempt   StepAttempt   `msgpack:"a"`
	KVRevs    *KVRevs       `msgpack:"kvs,omitempty"`
	Msg       []byte        `msgpack:"m"`
}

// EncodeMsgpack implements custom msgpack encoding for Directive
func (d *Directive) EncodeMsgpack(enc *msgpack.Encoder) error {
	if d.ID == "" {
		return enc.Encode(&directiveWire{})
	}
	if d.Msg == nil {
		return fmt.Errorf("directive message cannot be nil")
	}

	nxBytes, err := msgpack.Marshal(d.Msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	wire := directiveWire{
		ID:        d.ID,
		Kind:      d.Kind,
		Timestamp: d.Timestamp,
		RunInfo:   d.RunInfo,
		Attempt:   d.Attempt,
		Msg:       nxBytes,
	}

	return enc.Encode(&wire)
}

func (d *Directive) DecodeMsgpack(dec *msgpack.Decoder) error {
	var wire directiveWire
	if err := dec.Decode(&wire); err != nil {
		slog.Error("Failed to decode directive wire format", "error", err)
		return fmt.Errorf("failed to decode directive: %w", err)
	}

	if wire.ID == "" {
		*d = Directive{}
		return nil
	}

	factory, ok := DirectiveFactories[wire.Kind]
	if !ok {
		return fmt.Errorf("unknown directive kind: %s", wire.Kind)
	}

	nxDirective := factory()
	if err := msgpack.Unmarshal(wire.Msg, nxDirective); err != nil {
		slog.Error("Failed to unmarshal directive message",
			"error", err,
			"kind", wire.Kind,
			"msg_bytes_len", len(wire.Msg))
		return fmt.Errorf("failed to unmarshal message for kind %s: %w", wire.Kind, err)
	}

	*d = Directive{
		ID:        wire.ID,
		Kind:      wire.Kind,
		Timestamp: wire.Timestamp,
		RunInfo:   wire.RunInfo,
		Attempt:   wire.Attempt,
		Msg:       nxDirective,
	}

	return nil
}

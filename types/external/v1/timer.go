package external

import (
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

type TimerKind string
type TimerID string

func NewTimerID() TimerID {
	return TimerID(ulid.Make().String())
}

// DeriveTimerID produces a deterministic TimerID from the directive that triggered the timer.
// This allows the timer to be canceled later using only the directive ID, without extra storage.
func DeriveTimerID(directiveID DirectiveID, kind TimerKind) TimerID {
	return TimerID(fmt.Sprintf("%s.%s", kind, directiveID))
}

const (
	TimerKindSleep            TimerKind = "sleep"
	TimerKindSleepUntil       TimerKind = "sleep_until"
	TimerKindWaitEventTimeout TimerKind = "wait_event_timeout"
	TimerKindStepTimeout      TimerKind = "step_timeout"
)

// Timer represents a scheduled action to be executed at a specific time.
// Timers are published to NATS with scheduled delivery headers and contain
// the complete Directive or Command to dispatch when they expire.
type Timer struct {
	ID        TimerID   `json:"id" msgpack:"id"`
	Kind      TimerKind `json:"kind" msgpack:"k"`
	WFID      WFID      `json:"wf_id" msgpack:"wfid"`
	CreatedAt time.Time `json:"created_at" msgpack:"created"`
	ExpiresAt time.Time `json:"expires_at" msgpack:"expires"`

	// Context for debugging and logging
	StepName string `json:"step_name,omitempty" msgpack:"step,omitempty"`

	// The message to dispatch on expiry (one will be set)
	Directive Directive `json:"directive" msgpack:"d"`
	Command   Command   `json:"command" msgpack:"c"`
}

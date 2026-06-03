package types

import (
	"strings"
	"time"

	ext "grctl/server/types/external/v1"
)

// CommitResult describes the outcome of a Commit call so callers can
// distinguish storage-level conditions (CAS rejection, duplicate) from
// infrastructure errors without importing the store package.
type CommitResult struct {
	IsCASRejection     bool
	IsDuplicateMessage bool
}

// StateSnapshot holds the current state of a run as seen by the run manager.
type StateSnapshot struct {
	RunState ext.RunState
	Event    ext.Directive
}

// HandlerAction defines the outcome of processing a directive
type HandlerAction int

const (
	// processing failed permanently (don't retry)
	ActionFailed HandlerAction = iota

	// the directive was successfully handled
	ActionProcessed

	// processing failed but should be retried
	ActionRetryable
)

// HandleResult represents the outcome of processing a directive
type HandleResult struct {
	Action     HandlerAction
	RetryDelay time.Duration // Used only for ActionRetryable
	Message    string        // Used only for ActionFailed
}

// Processed creates a HandleResult indicating successful processing
func Processed() HandleResult {
	return HandleResult{Action: ActionProcessed}
}

func (r HandleResult) IsProcessed() bool {
	return r.Action == ActionProcessed
}

// Retryable creates a HandleResult indicating a retryable failure
func Retryable(delay time.Duration) HandleResult {
	return HandleResult{
		Action:     ActionRetryable,
		RetryDelay: delay,
	}
}

// Failed creates a HandleResult indicating a permanent failure.
// Message is always normalized to a non-empty string for history propagation.
func Failed(msg string) HandleResult {
	if strings.TrimSpace(msg) == "" {
		msg = "unknown permanent failure"
	}
	return HandleResult{
		Action:  ActionFailed,
		Message: msg,
	}
}

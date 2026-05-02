package machine

import (
	"strings"
	"time"
)

// HandlerAction defines the outcome of processing a directive
type HandlerAction int

const (
	// ActionFailed means processing failed permanently (don't retry)
	ActionFailed HandlerAction = iota

	// ActionProcessed means the directive was successfully handled
	ActionProcessed

	// ActionRetryable means processing failed but should be retried
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

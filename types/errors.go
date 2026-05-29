package types

import "errors"

var (
	ErrWorkflowAlreadyRunning = errors.New("workflow already has an active run")
	ErrWorkflowRunNotFound    = errors.New("workflow run not found")
	ErrRunTerminal            = errors.New("workflow run is in a terminal state")
)

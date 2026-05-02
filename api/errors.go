package api

import (
	"encoding/json"
	"errors"
	"log/slog"

	"grctl/server/machine"
	"grctl/server/store"
)

// grctlError represents a structured API error response
type grctlError struct {
	Code    int    `json:"code" msgpack:"code"`
	Message string `json:"message" msgpack:"message"`
	Detail  string `json:"detail,omitempty" msgpack:"detail,omitempty"`
}

func (e grctlError) Error() string {
	return e.Message + ": " + e.Detail
}

// ApiErrorMap maps sentinel errors to structured grctlError responses
var ApiErrorMap = map[error]grctlError{
	ErrUnknownAPICommand: {
		Code:    4000,
		Message: "Unknown API command",
	},
	store.ErrWorkflowAlreadyRunning: {
		Code:    4001,
		Message: "Workflow already has an active run",
	},
	store.ErrWorkflowRunNotFound: {
		Code:    4002,
		Message: "Workflow run not found",
	},
	machine.ErrRunTerminal: {
		Code:    4003,
		Message: "Workflow run has already finished and cannot accept new commands",
	},
	ErrInvalidMessageType: {
		Code:    4005,
		Message: "Invalid message type",
	},
}

// NewgrctlError creates a grctlError from a Go error.
// If the error matches a sentinel error in ApiErrorMap (including wrapped errors),
// it uses the mapped error code and message. Otherwise, it creates a generic error.
func NewgrctlError(err error) grctlError {
	if err == nil {
		return grctlError{
			Code:    5000,
			Message: "Unknown error",
		}
	}

	// Check if error (or wrapped error) is in the map
	for sentinelErr, mappedErr := range ApiErrorMap {
		if errors.Is(err, sentinelErr) {
			mappedErr.Detail = err.Error() // preserve wrapped context
			return mappedErr
		}
	}

	// Default fallback for unmapped errors
	return grctlError{
		Code:    5000,
		Message: err.Error(),
	}
}

// ToJSON serializes the grctlError to JSON bytes.
// If marshaling fails, returns a static error JSON.
func (e grctlError) ToJSON() []byte {
	data, err := json.Marshal(e)
	if err != nil {
		slog.Error("failed to marshal error response", "error", err)
		return []byte(`{"code":5000,"message":"internal server error"}`)
	}
	return data
}

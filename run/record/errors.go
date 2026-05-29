package record

import "errors"

var (
	ErrRunAlreadyCompleted = errors.New("run is already completed")
	ErrStaleDirective      = errors.New("stale step timeout directive")
)

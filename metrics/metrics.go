package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
)

// Recorder is the interface for recording server-side metrics.
// Use NewOTelRecorder for a real implementation and NewNoopRecorder for tests.
type Recorder interface {
	RecordRunStarted(ctx context.Context, wfType string)
	RecordRunTerminal(ctx context.Context, wfType string, outcome string)
	RecordDirectiveHandle(ctx context.Context, kind string, duration time.Duration)
	RecordDirectiveCommit(ctx context.Context, kind string, duration time.Duration)
	RecordAPICommand(ctx context.Context, kind string, success bool)
	RecordBgTask(ctx context.Context, kind string, result string)
}

func NewNoopRecorder() Recorder {
	return NewOTelRecorder(noop.NewMeterProvider())
}

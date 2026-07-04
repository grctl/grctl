package metrics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "grctl.server"

type OTelRecorder struct {
	runsStarted     metric.Int64Counter
	runsTerminal    metric.Int64Counter
	directiveHandle metric.Float64Histogram
	directiveCommit metric.Float64Histogram
	apiCommands     metric.Int64Counter
	bgTasks         metric.Int64Counter
}

func NewOTelRecorder(mp metric.MeterProvider) *OTelRecorder {
	m := mp.Meter(meterName)

	runsStarted, _ := m.Int64Counter("grctl.runs.started",
		metric.WithDescription("Total workflow runs started"),
		metric.WithUnit("{run}"),
	)
	runsTerminal, _ := m.Int64Counter("grctl.runs.terminal",
		metric.WithDescription("Total workflow runs that reached a terminal state"),
		metric.WithUnit("{run}"),
	)
	directiveHandle, _ := m.Float64Histogram("grctl.directive.handle.duration",
		metric.WithDescription("Duration of full directive handling (snapshot + plan + commit)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0002, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25),
	)
	directiveCommit, _ := m.Float64Histogram("grctl.directive.commit.duration",
		metric.WithDescription("Duration of the JetStream commit within directive handling"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.0002, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25),
	)
	apiCommands, _ := m.Int64Counter("grctl.api.commands",
		metric.WithDescription("Total API commands received"),
		metric.WithUnit("{command}"),
	)
	bgTasks, _ := m.Int64Counter("grctl.bg_tasks.processed",
		metric.WithDescription("Total background tasks processed"),
		metric.WithUnit("{task}"),
	)

	return &OTelRecorder{
		runsStarted:     runsStarted,
		runsTerminal:    runsTerminal,
		directiveHandle: directiveHandle,
		directiveCommit: directiveCommit,
		apiCommands:     apiCommands,
		bgTasks:         bgTasks,
	}
}

func (r *OTelRecorder) RecordRunStarted(ctx context.Context, wfType string) {
	r.runsStarted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("wf_type", wfType),
	))
}

func (r *OTelRecorder) RecordRunTerminal(ctx context.Context, wfType string, outcome string) {
	r.runsTerminal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("wf_type", wfType),
		attribute.String("outcome", outcome),
	))
}

func (r *OTelRecorder) RecordDirectiveHandle(ctx context.Context, kind string, duration time.Duration) {
	r.directiveHandle.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("kind", kind),
	))
}

func (r *OTelRecorder) RecordDirectiveCommit(ctx context.Context, kind string, duration time.Duration) {
	r.directiveCommit.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("kind", kind),
	))
}

func (r *OTelRecorder) RecordAPICommand(ctx context.Context, kind string, success bool) {
	result := "success"
	if !success {
		result = "error"
	}
	r.apiCommands.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("result", result),
	))
}

func (r *OTelRecorder) RecordBgTask(ctx context.Context, kind string, result string) {
	r.bgTasks.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("result", result),
	))
}

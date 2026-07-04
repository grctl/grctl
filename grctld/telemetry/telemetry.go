package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type ShutdownFunc func(ctx context.Context) error

// NewMeterProvider creates an OTLP gRPC metric provider that pushes to endpoint.
// The caller must call the returned ShutdownFunc to flush and close the exporter
// on process exit.
func NewMeterProvider(ctx context.Context, endpoint string, insecure bool, svcName string) (*metric.MeterProvider, ShutdownFunc, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}

	exp, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			attribute.String("service.name", svcName),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTel resource: %w", err)
	}

	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exp,
			metric.WithInterval(30*time.Second),
		)),
		metric.WithResource(res),
	)

	return mp, mp.Shutdown, nil
}

// Package telemetry wires OpenTelemetry tracing through the deployment
// pipeline: a span opened by the API when a deployment is created continues
// inside the worker that executes it, because the W3C traceparent travels in
// the outbox payload alongside the job event.
//
// Tracing activates when OTEL_EXPORTER_OTLP_ENDPOINT is set (OTLP/HTTP);
// otherwise every operation is a no-op with zero overhead.
package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/tatitoxt/shippulse"

// Setup installs the global TracerProvider when OTEL_EXPORTER_OTLP_ENDPOINT
// is present and returns a shutdown function. Without the env var it installs
// nothing: spans are no-ops and callers need not branch.
func Setup(ctx context.Context, service string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL, semconv.ServiceName(service))),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Tracer returns the package-level tracer; a no-op when Setup ran without an
// OTLP endpoint.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// TraceParent serializes the active span context of ctx as a W3C traceparent,
// or "" when no recording span is active — safe to call unconditionally.
func TraceParent(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	var carrier propagation.MapCarrier = map[string]string{}
	propagation.TraceContext{}.Inject(ctx, &carrier)
	return carrier["traceparent"]
}

// ExtractTraceParent returns a context whose remote span context is decoded
// from a W3C traceparent string carried across the bus.
func ExtractTraceParent(ctx context.Context, tp string) context.Context {
	if tp == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{"traceparent": tp}
	return propagation.TraceContext{}.Extract(ctx, carrier)
}

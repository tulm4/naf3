// Package tracing provides OpenTelemetry setup for NSSAAF.
// REQ-17: Full cross-component OTel tracing via W3C TraceContext.
// D-01: Biz Pod is the trace correlation hub.
package tracing

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Init initializes OpenTelemetry with W3C TraceContext propagation.
// D-01: Full cross-component tracing. Call from main.go during startup.
// Returns a shutdown function to flush traces on graceful shutdown.
func Init(serviceName, version, podID string) (shutdown func()) {
	// W3C TraceContext propagator — D-01
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	// stdout exporter — swap for OTLP exporter in production
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		panic("tracing: failed to create exporter: " + err.Error())
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
			attribute.String("pod.name", podID),
		)),
	)
	otel.SetTracerProvider(tp)

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}
}

// HTTPTransport returns an OTel-instrumented HTTP transport.
// Use this instead of http.DefaultTransport for NF client HTTP clients.
// REQ-17: Automatic span creation for all HTTP calls.
func HTTPTransport() http.RoundTripper {
	return otelhttp.NewTransport(http.DefaultTransport)
}

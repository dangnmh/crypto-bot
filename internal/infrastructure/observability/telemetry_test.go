package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/observability"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// ──────────────────────────────────────────────────────────────────────
// TraceHandler tests
// ──────────────────────────────────────────────────────────────────────.

func TestTraceHandler_InjectsTraceIDAndSpanID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := observability.NewTraceHandler(inner)
	logger := slog.New(handler)

	// Create a real trace span.
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-op")
	defer span.End()

	logger.InfoContext(ctx, "hello from span")

	var record map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse log: %v", err)
	}

	if _, ok := record["trace_id"]; !ok {
		t.Error("expected trace_id in log output")
	}
	if _, ok := record["span_id"]; !ok {
		t.Error("expected span_id in log output")
	}

	// Verify they are non-empty.
	if record["trace_id"] == "" {
		t.Error("trace_id should not be empty")
	}
	if record["span_id"] == "" {
		t.Error("span_id should not be empty")
	}
}

func TestTraceHandler_InjectsReqID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := observability.NewTraceHandler(inner)
	logger := slog.New(handler)

	ctx := observability.WithCorrelationID(context.Background())
	expectedReqID := observability.CorrelationID(ctx)

	logger.InfoContext(ctx, "hello with req_id")

	var record map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse log: %v", err)
	}

	if reqID, ok := record["req_id"]; !ok {
		t.Error("expected req_id in log output")
	} else if reqID != expectedReqID {
		t.Errorf("req_id mismatch: got %q, want %q", reqID, expectedReqID)
	}
}

func TestTraceHandler_NoSpan_NoTraceFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := observability.NewTraceHandler(inner)
	logger := slog.New(handler)

	// No span, no correlation ID — plain context.
	logger.InfoContext(context.Background(), "plain log")

	var record map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse log: %v", err)
	}

	if _, ok := record["trace_id"]; ok {
		t.Error("trace_id should NOT be present when no span is active")
	}
	if _, ok := record["req_id"]; ok {
		t.Error("req_id should NOT be present when no correlation ID is set")
	}
}

func TestTraceHandler_BothSpanAndReqID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := observability.NewTraceHandler(inner)
	logger := slog.New(handler)

	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-op")
	defer span.End()

	ctx = observability.WithCorrelationID(ctx)

	logger.InfoContext(ctx, "full context")

	var record map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse log: %v", err)
	}

	for _, key := range []string{"trace_id", "span_id", "req_id"} {
		if _, ok := record[key]; !ok {
			t.Errorf("expected %s in log output", key)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────
// PrometheusCollector tests
// ──────────────────────────────────────────────────────────────────────.

func TestPrometheusCollector_Counter(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test",
		MetricsPort: 0, // No HTTP server
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)

	// Should not panic.
	collector.Counter("test_counter", 1, map[string]string{"symbol": "BTC_USDT"})
	collector.Counter("test_counter", 5, map[string]string{"symbol": "BTC_USDT"})
}

func TestPrometheusCollector_Gauge(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)
	collector.Gauge("test_gauge", 42.5, map[string]string{"symbol": "ETH_USDT"})
}

func TestPrometheusCollector_Histogram(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)
	collector.Histogram("test_latency", 15.5, nil)
	collector.Histogram("test_latency", 22.3, nil)
}

func TestPrometheusCollector_Timer(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)
	collector.Timer("test_op", 150_000_000, nil) // 150ms
}

// ──────────────────────────────────────────────────────────────────────
// Telemetry init tests
// ──────────────────────────────────────────────────────────────────────.

func TestInitTelemetry_NoPort(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test-svc",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	if tel.Tracer == nil {
		t.Error("expected Tracer to be initialized")
	}
	if tel.TracerProvider == nil {
		t.Error("expected TracerProvider to be initialized")
	}
}

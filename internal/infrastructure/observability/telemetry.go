package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ──────────────────────────────────────────────────────────────────────
// Telemetry — single initialization point for OTel + Prometheus
// ──────────────────────────────────────────────────────────────────────.

// TelemetryConfig holds configuration for the observability stack.
type TelemetryConfig struct {
	ServiceName string // e.g. "crypto-bot-funding" or "crypto-bot-penny"
	MetricsPort int    // HTTP port for /metrics endpoint (0 = disabled)
}

// Telemetry holds the initialized OTel providers and HTTP server.
type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	Tracer         trace.Tracer
	Meter          otelmetric.Meter
	metricsServer  *http.Server
}

// InitTelemetry sets up:
//   - OpenTelemetry TracerProvider (in-process, no exporter — traces live in logs via trace_id)
//   - Prometheus MeterProvider (exports metrics on /metrics endpoint)
//   - Global OTel tracer + meter
//
// Returns a Telemetry struct and a shutdown function.
func InitTelemetry(cfg TelemetryConfig) (*Telemetry, func(context.Context) error) {
	// ── Tracer Provider ──
	// We use an in-process tracer that generates trace_id/span_id.
	// These IDs are injected into slog via the TraceHandler.
	// No external exporter needed — the traces are "log-based".
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	// ── Prometheus Meter Provider ──
	promExporter, err := prometheus.New()
	if err != nil {
		slog.Error("❌ Failed to create Prometheus exporter", "error", err)
		// Fall back to no-op: still return a valid Telemetry.
		return fallbackTelemetry(tp, cfg)
	}

	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(promExporter))
	otel.SetMeterProvider(mp)

	tracer := tp.Tracer(cfg.ServiceName)
	meter := mp.Meter(cfg.ServiceName)

	tel := &Telemetry{
		TracerProvider: tp,
		MeterProvider:  mp,
		Tracer:         tracer,
		Meter:          meter,
	}

	// ── Prometheus HTTP Server ──
	if cfg.MetricsPort > 0 {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})

		tel.metricsServer = &http.Server{
			Addr:              fmt.Sprintf(":%d", cfg.MetricsPort),
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}

		go func() {
			slog.Info("📊 Prometheus metrics server started", "port", cfg.MetricsPort, "endpoint", "/metrics")
			if err := tel.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("❌ Metrics server error", "error", err)
			}
		}()
	}

	shutdown := func(ctx context.Context) error {
		var firstErr error
		if tel.metricsServer != nil {
			if err := tel.metricsServer.Shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := mp.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := tp.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	return tel, shutdown
}

func fallbackTelemetry(tp *sdktrace.TracerProvider, cfg TelemetryConfig) (*Telemetry, func(context.Context) error) {
	return &Telemetry{
			TracerProvider: tp,
			Tracer:         tp.Tracer(cfg.ServiceName),
		}, func(ctx context.Context) error {
			return tp.Shutdown(ctx)
		}
}

package observability_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/observability"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitTelemetry_WithPort(t *testing.T) {
	t.Parallel()
	// Use an ephemeral port.
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test-with-port",
		MetricsPort: 0, // No server
	})
	require.NotNil(t, tel)
	require.NotNil(t, shutdown)

	assert.NotNil(t, tel.Tracer)
	assert.NotNil(t, tel.MeterProvider)
	assert.NotNil(t, tel.Meter)
	assert.NotNil(t, tel.TracerProvider)

	err := shutdown(context.Background())
	assert.NoError(t, err)
}

func TestPrometheusCollector_NilTags(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)

	// All methods should accept nil tags.
	assert.NotPanics(t, func() {
		collector.Counter("nil_tags_counter", 1, nil)
		collector.Gauge("nil_tags_gauge", 1.0, nil)
		collector.Histogram("nil_tags_histogram", 1.0, nil)
		collector.Timer("nil_tags_timer", 100, nil)
	})
}

func TestPrometheusCollector_MultipleTags(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)
	tags := map[string]string{"symbol": "BTC_USDT", "side": "buy", "strategy": "funding"}

	assert.NotPanics(t, func() {
		collector.Counter("multi_tags", 1, tags)
		collector.Gauge("multi_tags", 42.0, tags)
	})
}

func TestPrometheusCollector_CacheHit(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)

	// Call twice with same name to test getOrCreate cache.
	assert.NotPanics(t, func() {
		collector.Counter("cached_counter", 1, nil)
		collector.Counter("cached_counter", 2, nil) // cache hit
		collector.Gauge("cached_gauge", 1.0, nil)
		collector.Gauge("cached_gauge", 2.0, nil) // cache hit
		collector.Histogram("cached_histo", 1.0, nil)
		collector.Histogram("cached_histo", 2.0, nil) // cache hit
	})
}

func TestPrometheusCollector_Timer_WithTags(t *testing.T) {
	t.Parallel()
	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test-timer",
		MetricsPort: 0,
	})
	defer func() { _ = shutdown(context.Background()) }()

	collector := observability.NewPrometheusCollector(tel.Meter)

	// Timer appends "_ms" suffix and records milliseconds on a histogram.
	assert.NotPanics(t, func() {
		collector.Timer("api.latency", 100*time.Millisecond, map[string]string{"endpoint": "/ping"})
		collector.Timer("api.latency", 200*time.Millisecond, nil)
	})
}

func TestInitTelemetry_WithMetricsServer(t *testing.T) {
	t.Parallel()

	// Use a unique port for this test to avoid conflicts.
	port := 29876

	tel, shutdown := observability.InitTelemetry(observability.TelemetryConfig{
		ServiceName: "test-server",
		MetricsPort: port,
	})
	require.NotNil(t, tel)
	defer func() { _ = shutdown(context.Background()) }()

	// Wait for the server to be ready with retries.
	var resp *http.Response
	var err error
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost:29876/health", http.NoBody)
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			break
		}
	}
	require.NoError(t, err, "metrics server should start within 1s")

	// Record some metrics to ensure endpoint returns data.
	collector := observability.NewPrometheusCollector(tel.Meter)
	collector.Counter("test_metric", 1, nil)

	// Hit /metrics endpoint.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost:29876/metrics", http.NoBody)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, 200, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "target_info")

	// Hit /health endpoint.
	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost:29876/health", http.NoBody)
	healthResp, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer func() { _ = healthResp.Body.Close() }()
	assert.Equal(t, 200, healthResp.StatusCode)

	healthBody, _ := io.ReadAll(healthResp.Body)
	assert.Equal(t, "ok", string(healthBody))
}

func TestHealthChecker_UnregisteredComponent_NoOp(t *testing.T) {
	t.Parallel()
	h := observability.NewHealthChecker()

	// Setting healthy on unregistered component should be a no-op.
	assert.NotPanics(t, func() {
		h.SetHealthy("non-existent")
		h.SetUnhealthy("non-existent", "err")
	})

	overall, components := h.Check(context.Background())
	assert.True(t, overall, "no components means overall healthy")
	assert.Empty(t, components)
}

func TestHealthChecker_MessageRetrieval(t *testing.T) {
	t.Parallel()
	h := observability.NewHealthChecker()
	h.Register("db")
	h.SetUnhealthy("db", "connection refused")

	_, components := h.Check(context.Background())
	require.Len(t, components, 1)
	assert.Equal(t, "connection refused", components[0].Message)
	assert.False(t, components[0].Healthy)

	// Now set healthy and verify message is cleared.
	h.SetHealthy("db")
	_, components = h.Check(context.Background())
	require.Len(t, components, 1)
	assert.True(t, components[0].Healthy)
	assert.Empty(t, components[0].Message)
}

package observability_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/observability"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInMemoryCollector_Counter(t *testing.T) {
	t.Parallel()
	c := observability.NewInMemoryCollector()
	c.Counter("orders.placed", 1, nil)
	c.Counter("orders.placed", 1, nil)

	assert.Equal(t, int64(2), c.GetCounter("orders.placed"))
}

func TestInMemoryCollector_Gauge(t *testing.T) {
	t.Parallel()
	c := observability.NewInMemoryCollector()
	c.Gauge("balance", 1000.5, nil)
	c.Gauge("balance", 999.0, nil)

	assert.Equal(t, 999.0, c.GetGauge("balance"))
}

func TestInMemoryCollector_Histogram(t *testing.T) {
	t.Parallel()
	c := observability.NewInMemoryCollector()
	c.Histogram("latency_ms", 15.0, nil)
	c.Histogram("latency_ms", 25.0, nil)

	c.RLock()
	h := c.Histograms["latency_ms"]
	c.RUnlock()

	assert.Len(t, h, 2)
}

func TestInMemoryCollector_Timer(t *testing.T) {
	t.Parallel()
	c := observability.NewInMemoryCollector()
	c.Timer("api.call", 50*time.Millisecond, nil)

	c.RLock()
	timers := c.Timers["api.call"]
	c.RUnlock()

	assert.Len(t, timers, 1)
	assert.Equal(t, 50*time.Millisecond, timers[0])
}

func TestNoopCollector_DoesNotPanic(t *testing.T) {
	t.Parallel()
	c := &observability.NoopCollector{}
	assert.NotPanics(t, func() {
		c.Counter("test", 1, nil)
		c.Gauge("test", 1.0, nil)
		c.Histogram("test", 1.0, nil)
		c.Timer("test", time.Second, nil)
	})
}

func TestHealthChecker_AllHealthy(t *testing.T) {
	t.Parallel()
	h := observability.NewHealthChecker()
	h.Register("timesync")
	h.Register("ws")

	h.SetHealthy("timesync")
	h.SetHealthy("ws")

	overall, components := h.Check(context.Background())
	assert.True(t, overall)
	assert.Len(t, components, 2)
}

func TestHealthChecker_OneUnhealthy(t *testing.T) {
	t.Parallel()
	h := observability.NewHealthChecker()
	h.Register("timesync")
	h.Register("ws")

	h.SetHealthy("timesync")
	h.SetUnhealthy("ws", "connection lost")

	overall, _ := h.Check(context.Background())
	assert.False(t, overall)
}

func TestCorrelationID_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := observability.WithCorrelationID(context.Background())
	cid := observability.CorrelationID(ctx)

	assert.NotEmpty(t, cid)
	assert.Len(t, cid, 8)
}

func TestCorrelationID_WithValue(t *testing.T) {
	t.Parallel()
	ctx := observability.WithCorrelationIDValue(context.Background(), "test-123")
	assert.Equal(t, "test-123", observability.CorrelationID(ctx))
}

func TestCorrelationID_Missing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	assert.Empty(t, observability.CorrelationID(ctx))
}

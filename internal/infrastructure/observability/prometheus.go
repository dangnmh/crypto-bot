package observability

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

// ──────────────────────────────────────────────────────────────────────
// PrometheusCollector — MetricsCollector backed by OTel → Prometheus
// ──────────────────────────────────────────────────────────────────────.

// PrometheusCollector implements MetricsCollector using OpenTelemetry's
// metric SDK, which exports to Prometheus via the /metrics endpoint.
//
// Instruments are lazily created and cached by name. This allows callers
// to use any metric name without pre-registration.
type PrometheusCollector struct {
	meter      otelmetric.Meter
	mu         sync.RWMutex
	counters   map[string]otelmetric.Int64Counter
	gauges     map[string]otelmetric.Float64Gauge
	histograms map[string]otelmetric.Float64Histogram
}

// NewPrometheusCollector creates a PrometheusCollector backed by the given OTel Meter.
func NewPrometheusCollector(meter otelmetric.Meter) *PrometheusCollector {
	return &PrometheusCollector{
		meter:      meter,
		counters:   make(map[string]otelmetric.Int64Counter),
		gauges:     make(map[string]otelmetric.Float64Gauge),
		histograms: make(map[string]otelmetric.Float64Histogram),
	}
}

// Counter increments a named counter.
func (p *PrometheusCollector) Counter(name string, value int64, tags map[string]string) {
	c := p.getOrCreateCounter(name)
	c.Add(context.Background(), value, otelmetric.WithAttributes(tagsToAttrs(tags)...))
}

// Gauge sets a named gauge value.
func (p *PrometheusCollector) Gauge(name string, value float64, tags map[string]string) {
	g := p.getOrCreateGauge(name)
	g.Record(context.Background(), value, otelmetric.WithAttributes(tagsToAttrs(tags)...))
}

// Histogram records an observation for distribution analysis.
func (p *PrometheusCollector) Histogram(name string, value float64, tags map[string]string) {
	h := p.getOrCreateHistogram(name)
	h.Record(context.Background(), value, otelmetric.WithAttributes(tagsToAttrs(tags)...))
}

// Timer records a duration observation as milliseconds on a histogram.
func (p *PrometheusCollector) Timer(name string, d time.Duration, tags map[string]string) {
	h := p.getOrCreateHistogram(name + "_ms")
	h.Record(context.Background(), float64(d.Milliseconds()), otelmetric.WithAttributes(tagsToAttrs(tags)...))
}

// ── Lazy instrument creation ────────────────────────────────────────.

func (p *PrometheusCollector) getOrCreateCounter(name string) otelmetric.Int64Counter {
	p.mu.RLock()
	c, ok := p.counters[name]
	p.mu.RUnlock()
	if ok {
		return c
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after lock.
	if c, ok = p.counters[name]; ok {
		return c
	}

	c, _ = p.meter.Int64Counter(name)
	p.counters[name] = c
	return c
}

func (p *PrometheusCollector) getOrCreateGauge(name string) otelmetric.Float64Gauge {
	p.mu.RLock()
	g, ok := p.gauges[name]
	p.mu.RUnlock()
	if ok {
		return g
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if g, ok = p.gauges[name]; ok {
		return g
	}

	g, _ = p.meter.Float64Gauge(name)
	p.gauges[name] = g
	return g
}

func (p *PrometheusCollector) getOrCreateHistogram(name string) otelmetric.Float64Histogram {
	p.mu.RLock()
	h, ok := p.histograms[name]
	p.mu.RUnlock()
	if ok {
		return h
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if h, ok = p.histograms[name]; ok {
		return h
	}

	h, _ = p.meter.Float64Histogram(name)
	p.histograms[name] = h
	return h
}

// tagsToAttrs converts a map of tags to OTel attributes.
func tagsToAttrs(tags map[string]string) []attribute.KeyValue {
	if len(tags) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(tags))
	for k, v := range tags {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}

// Package observability provides metrics collection and health check interfaces.
// This package defines contracts at the domain level — concrete implementations
// (Prometheus, StatsD, etc.) belong in infrastructure.
package observability

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// MetricsCollector — generic metrics interface
// ──────────────────────────────────────────────────────────────────────.

// MetricsCollector provides a generic interface for recording operational metrics.
// Implementations can write to Prometheus, StatsD, or a simple in-memory store.
type MetricsCollector interface {
	// Counter increments a named counter by the given value.
	Counter(name string, value int64, tags map[string]string)
	// Gauge sets a named gauge to the given value.
	Gauge(name string, value float64, tags map[string]string)
	// Histogram records a named observation (e.g. latency) for distribution analysis.
	Histogram(name string, value float64, tags map[string]string)
	// Timer records the duration of an operation.
	Timer(name string, duration time.Duration, tags map[string]string)
}

// ──────────────────────────────────────────────────────────────────────
// NoopCollector — default no-op implementation
// ──────────────────────────────────────────────────────────────────────.

// NoopCollector is a no-op MetricsCollector for when metrics are disabled.
type NoopCollector struct{}

func (n *NoopCollector) Counter(_ string, _ int64, _ map[string]string)       {}
func (n *NoopCollector) Gauge(_ string, _ float64, _ map[string]string)       {}
func (n *NoopCollector) Histogram(_ string, _ float64, _ map[string]string)   {}
func (n *NoopCollector) Timer(_ string, _ time.Duration, _ map[string]string) {}

// ──────────────────────────────────────────────────────────────────────
// InMemoryCollector — simple in-memory metrics for testing/debugging
// ──────────────────────────────────────────────────────────────────────.

// InMemoryCollector stores metrics in memory for inspection during tests or debugging.
type InMemoryCollector struct {
	mu         sync.RWMutex
	Counters   map[string]int64
	Gauges     map[string]float64
	Histograms map[string][]float64
	Timers     map[string][]time.Duration
}

// NewInMemoryCollector creates a new in-memory metrics collector.
func NewInMemoryCollector() *InMemoryCollector {
	return &InMemoryCollector{
		Counters:   make(map[string]int64),
		Gauges:     make(map[string]float64),
		Histograms: make(map[string][]float64),
		Timers:     make(map[string][]time.Duration),
	}
}

func (c *InMemoryCollector) Counter(name string, value int64, _ map[string]string) {
	c.mu.Lock()
	c.Counters[name] += value
	c.mu.Unlock()
}

func (c *InMemoryCollector) Gauge(name string, value float64, _ map[string]string) {
	c.mu.Lock()
	c.Gauges[name] = value
	c.mu.Unlock()
}

func (c *InMemoryCollector) Histogram(name string, value float64, _ map[string]string) {
	c.mu.Lock()
	c.Histograms[name] = append(c.Histograms[name], value)
	c.mu.Unlock()
}

func (c *InMemoryCollector) Timer(name string, d time.Duration, _ map[string]string) {
	c.mu.Lock()
	c.Timers[name] = append(c.Timers[name], d)
	c.mu.Unlock()
}

// GetCounter returns the current counter value.
func (c *InMemoryCollector) GetCounter(name string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Counters[name]
}

// GetGauge returns the current gauge value.
func (c *InMemoryCollector) GetGauge(name string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Gauges[name]
}

// ──────────────────────────────────────────────────────────────────────
// HealthChecker — component health reporting
// ──────────────────────────────────────────────────────────────────────.

// ComponentHealth represents the health status of a single component.
type ComponentHealth struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Message string `json:"message,omitempty"`
}

// HealthChecker aggregates health from multiple components.
type HealthChecker struct {
	mu         sync.RWMutex
	components map[string]*componentState
}

type componentState struct {
	healthy atomic.Bool
	message atomic.Value // string
}

// NewHealthChecker creates a new HealthChecker.
func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		components: make(map[string]*componentState),
	}
}

// Register registers a new component for health tracking.
func (h *HealthChecker) Register(name string) {
	h.mu.Lock()
	h.components[name] = &componentState{}
	h.mu.Unlock()
}

// SetHealthy marks a component as healthy.
func (h *HealthChecker) SetHealthy(name string) {
	h.mu.RLock()
	if c, ok := h.components[name]; ok {
		c.healthy.Store(true)
		c.message.Store("")
	}
	h.mu.RUnlock()
}

// SetUnhealthy marks a component as unhealthy with a message.
func (h *HealthChecker) SetUnhealthy(name, message string) {
	h.mu.RLock()
	if c, ok := h.components[name]; ok {
		c.healthy.Store(false)
		c.message.Store(message)
	}
	h.mu.RUnlock()
}

// Check returns the health status of all registered components.
func (h *HealthChecker) Check(_ context.Context) (overall bool, components []ComponentHealth) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	overall = true
	for name, state := range h.components {
		ch := ComponentHealth{
			Name:    name,
			Healthy: state.healthy.Load(),
		}
		if msg, ok := state.message.Load().(string); ok {
			ch.Message = msg
		}
		if !ch.Healthy {
			overall = false
		}
		components = append(components, ch)
	}
	return
}

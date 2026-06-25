package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	sysconfig "crypto-bot/internal/infrastructure/config"
)

// EngineBuilder provides a fluent API for constructing an Engine with validation.
type EngineBuilder struct {
	cfg        *sysconfig.SystemConfig
	httpClient *http.Client
	logger     *slog.Logger
	errors     []string
}

// NewEngineBuilder creates a new EngineBuilder.
func NewEngineBuilder() *EngineBuilder {
	return &EngineBuilder{}
}

// WithSystemConfig sets the system configuration (required).
func (b *EngineBuilder) WithSystemConfig(cfg *sysconfig.SystemConfig) *EngineBuilder {
	b.cfg = cfg
	return b
}

// WithHTTPClient sets the HTTP client connection pool (optional).
func (b *EngineBuilder) WithHTTPClient(client *http.Client) *EngineBuilder {
	b.httpClient = client
	return b
}

// WithLogger sets the logger used by the Engine and exchange providers.
func (b *EngineBuilder) WithLogger(logger *slog.Logger) *EngineBuilder {
	b.logger = logger
	return b
}

// Build validates all required fields and returns a configured Engine.
func (b *EngineBuilder) Build() (*Engine, error) {
	b.errors = nil

	if b.cfg == nil {
		b.errors = append(b.errors, "SystemConfig is required")
	} else {
		b.validateConfig()
	}
	if b.logger == nil {
		b.errors = append(b.errors, "Logger is required")
	}

	if len(b.errors) > 0 {
		return nil, fmt.Errorf("engine build failed: %v", b.errors)
	}

	return NewEngine(context.Background(), EngineConfig{
		SystemConfig: b.cfg,
		HTTPClient:   b.httpClient,
		Logger:       b.logger,
	})
}

// validateConfig checks if at least one exchange is enabled.
func (b *EngineBuilder) validateConfig() {
	if !b.cfg.HasEnabledExchange() {
		b.errors = append(b.errors, "at least one exchange must be enabled")
	}
}

package app

import (
	"fmt"
	"net/http"

	sysconfig "crypto-bot/internal/infrastructure/config"
)

// EngineBuilder provides a fluent API for constructing an Engine with validation.
type EngineBuilder struct {
	cfg        *sysconfig.SystemConfig
	httpClient *http.Client
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

// Build validates all required fields and returns a configured Engine.
func (b *EngineBuilder) Build() (*Engine, error) {
	b.errors = nil

	if b.cfg == nil {
		b.errors = append(b.errors, "SystemConfig is required")
	} else {
		b.validateConfig()
	}

	if len(b.errors) > 0 {
		return nil, fmt.Errorf("engine build failed: %v", b.errors)
	}

	return NewEngine(EngineConfig{
		SystemConfig: b.cfg,
		HTTPClient:   b.httpClient,
	}), nil
}

// validateConfig checks for mandatory config values for enabled exchanges.
func (b *EngineBuilder) validateConfig() {
	mexcEnabled := b.cfg.ExchangeConfig.Mexc.Future.BaseURL != ""
	gateEnabled := b.cfg.ExchangeConfig.Gate.Future.BaseURL != ""

	if !mexcEnabled && !gateEnabled {
		b.errors = append(b.errors, "at least one exchange must be configured (mexc or gate)")
		return
	}

	if mexcEnabled {
		if b.cfg.ExchangeConfig.Mexc.WebSocket.WSURL == "" {
			b.errors = append(b.errors, "MEXC API.WebSocket.WSURL is required")
		}
		if b.cfg.ExchangeConfig.Mexc.APIKey == "" {
			b.errors = append(b.errors, "MEXC APIKey is required")
		}
		if b.cfg.ExchangeConfig.Mexc.APISecret == "" {
			b.errors = append(b.errors, "MEXC APISecret is required")
		}
		if b.cfg.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn <= 0 {
			b.errors = append(b.errors, "MEXC MaxPairsPerWSConn must be greater than 0")
		}
	}

	if gateEnabled {
		if b.cfg.ExchangeConfig.Gate.WebSocket.WSURL == "" {
			b.errors = append(b.errors, "Gate API.WebSocket.WSURL is required")
		}
		if b.cfg.ExchangeConfig.Gate.APIKey == "" {
			b.errors = append(b.errors, "Gate APIKey is required")
		}
		if b.cfg.ExchangeConfig.Gate.APISecret == "" {
			b.errors = append(b.errors, "Gate APISecret is required")
		}
		if b.cfg.ExchangeConfig.Gate.WebSocket.MaxPairsPerWSConn <= 0 {
			b.errors = append(b.errors, "Gate MaxPairsPerWSConn must be greater than 0")
		}
	}
}

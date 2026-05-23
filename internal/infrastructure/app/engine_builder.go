package app

import (
	"fmt"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/ws"
)

// EngineBuilder provides a fluent API for constructing an Engine with validation.
// Required fields must be set before Build() is called.
type EngineBuilder struct {
	cfg     *sysconfig.SystemConfig
	client  exchange.Client
	adapter ws.ExchangeAdapter
	errors  []string
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

// WithClient sets the exchange REST client (required).
func (b *EngineBuilder) WithClient(client exchange.Client) *EngineBuilder {
	b.client = client
	return b
}

// WithAdapter sets the exchange WS adapter (required).
func (b *EngineBuilder) WithAdapter(adapter ws.ExchangeAdapter) *EngineBuilder {
	b.adapter = adapter
	return b
}

// Build validates all required fields and returns a configured Engine.
// Returns an error if any required field is missing or invalid.
func (b *EngineBuilder) Build() (*Engine, error) {
	b.errors = nil

	if b.cfg == nil {
		b.errors = append(b.errors, "SystemConfig is required")
	} else {
		b.validateConfig()
	}

	if b.client == nil {
		b.errors = append(b.errors, "exchange Client is required")
	}

	if b.adapter == nil {
		b.errors = append(b.errors, "WS ExchangeAdapter is required")
	}

	if len(b.errors) > 0 {
		return nil, fmt.Errorf("engine build failed: %v", b.errors)
	}

	return NewEngine(EngineConfig{
		SystemConfig: b.cfg,
		Client:       b.client,
		Adapter:      b.adapter,
	}), nil
}

// validateConfig checks for mandatory config values.
func (b *EngineBuilder) validateConfig() {
	if b.cfg.ExchangeConfig.Mexc.Future.BaseURL == "" {
		b.errors = append(b.errors, "API.Future.BaseURL is required")
	}
	if b.cfg.ExchangeConfig.Mexc.WebSocket.WSURL == "" {
		b.errors = append(b.errors, "API.WebSocket.WSURL is required")
	}
	if b.cfg.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn <= 0 {
		b.errors = append(b.errors, "API.WebSocket.MaxPairsPerWSConn must be > 0")
	}
	if b.cfg.ExchangeConfig.Mexc.APIKey == "" {
		b.errors = append(b.errors, "APIKey is required")
	}
	if b.cfg.ExchangeConfig.Mexc.APISecret == "" {
		b.errors = append(b.errors, "APISecret is required")
	}
}

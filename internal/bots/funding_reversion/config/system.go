package config

import (
	"encoding/json"
	"fmt"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/types"
)

// SystemConfig represents the system configuration for the Funding Reversion bot.
type SystemConfig struct {
	sysconfig.SystemConfig
	Safety          SafetyConfig    `json:"safety"`
	Sync            SyncConfig      `json:"sync"`
	TradingDefaults json.RawMessage `json:"tradingDefaults"`
}

type SyncConfig struct {
	sysconfig.SyncConfig
	// Funding Sync Interval
	FundingSync types.Duration `json:"funding"`
}

// SafetyConfig holds safety metrics specific to funding reversion.
type SafetyConfig struct {
	MaxCapitalPctPerSymbol float64        `json:"maxCapitalPctPerSymbol"`
	MaxImpactRatio         float64        `json:"maxImpactRatio"`
	MaxLatency             types.Duration `json:"maxLatency"`
	BufferTime             types.Duration `json:"bufferTime"`

	// Reversion strategy: hold position after entry
	HoldDuration types.Duration `json:"holdDuration"` // max time to hold position (e.g. "30s")
	// Hedge Trapping configurations
	TrapAfterSettle types.Duration `json:"trapAfterSettle"` // Duration after settle to throw trap

	// Post-settle safety
	PostSettleTimeout types.Duration `json:"postSettleTimeout"` // Max time to wait for position close (default 60s)
}

// LoadSystemConfig loads the system configuration from the given path.
func LoadSystemConfig(systemPath string) (*SystemConfig, error) {
	sysCfg, err := pkgconfig.Load[SystemConfig](systemPath)
	if err != nil {
		return nil, fmt.Errorf("load funding reversion system config: %w", err)
	}

	// Propagate unmarshaled sync to the base struct
	sysCfg.SystemConfig.Sync = sysCfg.Sync.SyncConfig

	if err := sysconfig.InitializeBase(&sysCfg.SystemConfig); err != nil {
		return nil, fmt.Errorf("initialize base config: %w", err)
	}

	// Propagate defaults back
	sysCfg.Sync.SyncConfig = sysCfg.SystemConfig.Sync

	if err := sysCfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return sysCfg, nil
}

func (c *SystemConfig) validate() error {
	// Apply bot specific safety defaults
	if c.Safety.BufferTime <= 0 {
		c.Safety.BufferTime = types.Duration(10 * 1e6) // 10ms
	}
	if c.Safety.HoldDuration <= 0 {
		c.Safety.HoldDuration = types.Duration(30 * 1e9) // 30s
	}
	if c.Safety.TrapAfterSettle <= 0 {
		c.Safety.TrapAfterSettle = types.Duration(10 * 1e6) // 10ms
	}
	if c.Safety.PostSettleTimeout <= 0 {
		c.Safety.PostSettleTimeout = types.Duration(60 * 1e9) // 60s
	}
	if c.Sync.FundingSync <= 0 {
		c.Sync.FundingSync = types.Duration(10 * 1e9) // 10s
	}

	// Normalize Global System percentages
	c.Safety.MaxCapitalPctPerSymbol /= 100
	c.Safety.MaxImpactRatio /= 100

	return nil
}

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"crypto-bot/pkg/types"

	"github.com/tailscale/hujson"
)

// Load reads funding.json and returns the Config.
func Load(sysCfg *SystemConfig, fundingPath string) (*Config, error) {
	fundData, err := os.ReadFile(fundingPath)
	if err != nil {
		return nil, fmt.Errorf("read funding config %s: %w", fundingPath, err)
	}

	fundData, err = hujson.Standardize(fundData)
	if err != nil {
		return nil, fmt.Errorf("standardize funding config json: %w", err)
	}

	var symCfgs []SymbolConfig
	if err := json.Unmarshal(fundData, &symCfgs); err != nil {
		return nil, fmt.Errorf("parse funding config: %w", err)
	}

	cfg := &Config{
		System:  sysCfg,
		Symbols: symCfgs,
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {
	var defaults TradingDefaults
	if c.System.TradingDefaults != nil {
		if err := json.Unmarshal(c.System.TradingDefaults, &defaults); err != nil {
			return fmt.Errorf("parse trading defaults: %w", err)
		}
	}

	if len(c.Symbols) == 0 {
		return fmt.Errorf("funding config empty — need at least one symbol")
	}

	for i := range c.Symbols {
		sc := &c.Symbols[i]
		if sc.Symbol == "" {
			return fmt.Errorf("symbol missing in funding config at index %d", i)
		}

		c.applyDefaults(sc, &defaults)

		if sc.MarginUSDT <= 0 {
			return fmt.Errorf("marginUSDT must be > 0 for %s", sc.Symbol)
		}
		if sc.Leverage < 1 {
			return fmt.Errorf("leverage >= 1 for %s", sc.Symbol)
		}

		c.normalizeSymbolMetrics(sc)
		c.defaultSymbolModes(sc)
	}

	return nil
}

func (c *Config) applyDefaults(sc *SymbolConfig, d *TradingDefaults) {
	defaultFloat(&sc.MaxPriceDiffPercent, d.MaxPriceDiffPercent)
	defaultFloat(&sc.MinFundingRate, d.MinFundingRate)
	defaultInt(&sc.Leverage, d.Leverage)
	defaultStr((*string)(&sc.OpenType), d.OpenType)
	defaultStr((*string)(&sc.PositionMode), d.PositionMode)

	if !sc.FundingReversion.Enabled && d.FundingReversion.Enabled {
		sc.FundingReversion = d.FundingReversion
	} else if sc.FundingReversion.Enabled {
		defaultFloat(&sc.FundingReversion.TakeProfitPct, d.FundingReversion.TakeProfitPct)
		defaultFloat(&sc.FundingReversion.StopLossPct, d.FundingReversion.StopLossPct)
		defaultDuration(&sc.FundingReversion.MaxLatency, d.FundingReversion.MaxLatency)
		defaultDuration(&sc.FundingReversion.BufferTime, d.FundingReversion.BufferTime)
		defaultDuration(&sc.FundingReversion.PostSettleTimeout, d.FundingReversion.PostSettleTimeout)
	}

	if !sc.FundingTrap.Enabled && d.FundingTrap.Enabled {
		sc.FundingTrap = d.FundingTrap
	} else if sc.FundingTrap.Enabled {
		defaultFloat(&sc.FundingTrap.SizeRatio, d.FundingTrap.SizeRatio)
		defaultFloat(&sc.FundingTrap.MaxNotionalUSDT, d.FundingTrap.MaxNotionalUSDT)
		defaultFloat(&sc.FundingTrap.DepthPct, d.FundingTrap.DepthPct)
		defaultFloat(&sc.FundingTrap.TakeProfitPct, d.FundingTrap.TakeProfitPct)
		defaultFloat(&sc.FundingTrap.StopLossPct, d.FundingTrap.StopLossPct)
		defaultDuration(&sc.FundingTrap.TrapAfterSettle, d.FundingTrap.TrapAfterSettle)
		defaultDuration(&sc.FundingTrap.PostSettleTimeout, d.FundingTrap.PostSettleTimeout)
		if !sc.FundingTrap.Trailing.Enabled && d.FundingTrap.Trailing.Enabled {
			sc.FundingTrap.Trailing = d.FundingTrap.Trailing
		}
	}
}

func (c *Config) normalizeSymbolMetrics(sc *SymbolConfig) {
	sc.MinFundingRate = normalizeFundingRateThreshold(sc.MinFundingRate)

	if sc.FundingReversion.Enabled {
		defaultDuration(&sc.FundingReversion.MaxLatency, types.Duration(200*time.Millisecond))
		defaultDuration(&sc.FundingReversion.BufferTime, types.Duration(10*time.Millisecond))
		defaultDuration(&sc.FundingReversion.PostSettleTimeout, types.Duration(60*time.Second))

		if sc.FundingReversion.TakeProfitPct <= 0 {
			sc.FundingReversion.TakeProfitPct = 20
		}
		if sc.FundingReversion.StopLossPct <= 0 {
			sc.FundingReversion.StopLossPct = 5
		}
		sc.FundingReversion.TakeProfitPct = normalizePercentRatio(sc.FundingReversion.TakeProfitPct)
		sc.FundingReversion.StopLossPct = normalizePercentRatio(sc.FundingReversion.StopLossPct)
	}

	if sc.FundingTrap.Enabled {
		if sc.FundingTrap.SizeRatio <= 0 {
			sc.FundingTrap.SizeRatio = 0.5
		}
		defaultDuration(&sc.FundingTrap.TrapAfterSettle, types.Duration(50*time.Millisecond))
		defaultDuration(&sc.FundingTrap.PostSettleTimeout, types.Duration(60*time.Second))

		if sc.FundingTrap.DepthPct <= 0 {
			sc.FundingTrap.DepthPct = 5
		}
		if sc.FundingTrap.TakeProfitPct <= 0 {
			sc.FundingTrap.TakeProfitPct = 2
		}
		if sc.FundingTrap.StopLossPct <= 0 {
			sc.FundingTrap.StopLossPct = 2
		}
		sc.FundingTrap.DepthPct = normalizePercentRatio(sc.FundingTrap.DepthPct)
		sc.FundingTrap.TakeProfitPct = normalizePercentRatio(sc.FundingTrap.TakeProfitPct)
		sc.FundingTrap.StopLossPct = normalizePercentRatio(sc.FundingTrap.StopLossPct)

		sc.FundingTrap.Trailing.ActivationPct = normalizePercentRatio(sc.FundingTrap.Trailing.ActivationPct)
		sc.FundingTrap.Trailing.CallbackPct = normalizePercentRatio(sc.FundingTrap.Trailing.CallbackPct)
	}
}

func (c *Config) defaultSymbolModes(sc *SymbolConfig) {
	switch strings.ToUpper(string(sc.OpenType)) {
	case string(OpenTypeIsolated):
		sc.ParsedOpenType = 1
	case "CROSS":
		sc.ParsedOpenType = 2
	default:
		sc.ParsedOpenType = 1
	}

	switch strings.ToUpper(string(sc.PositionMode)) {
	case string(PositionModeHedge):
		sc.ParsedPositionMode = 1
	case "ONE_WAY":
		sc.ParsedPositionMode = 2
	default:
		sc.ParsedPositionMode = 1
	}
}

// IsHedgeTrapEnabled returns true if hedge trap is enabled for this symbol config.
func (sc *SymbolConfig) IsHedgeTrapEnabled() bool {
	return sc.FundingTrap.Enabled
}

func defaultFloat(target *float64, fallback float64) {
	if *target == 0 {
		*target = fallback
	}
}

func defaultInt(target *int, fallback int) {
	if *target == 0 {
		*target = fallback
	}
}

func defaultDuration(target *types.Duration, fallback types.Duration) {
	if *target == 0 {
		*target = fallback
	}
}

func defaultStr(target *string, fallback string) {
	if *target == "" {
		*target = fallback
	}
}

// normalizePercentRatio converts user-facing percent values into internal
// ratios. Values already in ratio form are preserved for compatibility.
func normalizePercentRatio(v float64) float64 {
	if v <= 0 {
		return v
	}
	if v <= 0.2 {
		return v
	}
	return v / 100
}

// normalizeFundingRateThreshold accepts either 0.3 for 0.3% or 0.003 for
// 0.3%, then stores the internal exchange-style ratio.
func normalizeFundingRateThreshold(v float64) float64 {
	if v <= 0 {
		return v
	}
	if v <= 0.05 {
		return v
	}
	return v / 100
}

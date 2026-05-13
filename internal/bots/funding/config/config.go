package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"crypto-bot/internal/bots/funding/domain"

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
		if !sc.FundingReversion.DynamicPricing.Enabled && d.FundingReversion.DynamicPricing.Enabled {
			sc.FundingReversion.DynamicPricing = d.FundingReversion.DynamicPricing
		}
		if !sc.FundingReversion.Trailing.Enabled && d.FundingReversion.Trailing.Enabled {
			sc.FundingReversion.Trailing = d.FundingReversion.Trailing
		}
	}

	if !sc.FundingTrap.Enabled && d.FundingTrap.Enabled {
		sc.FundingTrap = d.FundingTrap
	} else if sc.FundingTrap.Enabled {
		defaultFloat(&sc.FundingTrap.DepthPct, d.FundingTrap.DepthPct)
		defaultFloat(&sc.FundingTrap.TakeProfitPct, d.FundingTrap.TakeProfitPct)
		defaultFloat(&sc.FundingTrap.StopLossPct, d.FundingTrap.StopLossPct)
		if !sc.FundingTrap.Trailing.Enabled && d.FundingTrap.Trailing.Enabled {
			sc.FundingTrap.Trailing = d.FundingTrap.Trailing
		}
	}
}

func (c *Config) normalizeSymbolMetrics(sc *SymbolConfig) {
	sc.MaxPriceDiffPercent /= 100

	sc.MinFundingRate /= 100

	if sc.FundingReversion.Enabled {
		if sc.FundingReversion.TakeProfitPct <= 0 {
			sc.FundingReversion.TakeProfitPct = 20
		}
		if sc.FundingReversion.StopLossPct <= 0 {
			sc.FundingReversion.StopLossPct = 5
		}
		sc.FundingReversion.TakeProfitPct /= 100
		sc.FundingReversion.StopLossPct /= 100

		sc.FundingReversion.Trailing.ActivationPct /= 100
		sc.FundingReversion.Trailing.CallbackPct /= 100

		if sc.FundingReversion.DynamicPricing.Enabled {
			setTrailingDynamicDefaults(&sc.FundingReversion.Trailing)
		}
	}

	if sc.FundingTrap.Enabled {
		if sc.FundingTrap.DepthPct <= 0 {
			sc.FundingTrap.DepthPct = 5
		}
		if sc.FundingTrap.TakeProfitPct <= 0 {
			sc.FundingTrap.TakeProfitPct = 2
		}
		if sc.FundingTrap.StopLossPct <= 0 {
			sc.FundingTrap.StopLossPct = 2
		}
		sc.FundingTrap.DepthPct /= 100
		sc.FundingTrap.TakeProfitPct /= 100
		sc.FundingTrap.StopLossPct /= 100

		sc.FundingTrap.Trailing.ActivationPct /= 100
		sc.FundingTrap.Trailing.CallbackPct /= 100

		if sc.FundingReversion.DynamicPricing.Enabled {
			setTrapDynamicDefaults(&sc.FundingTrap)
		}
	}
}

func setTrapDynamicDefaults(trap *domain.FundingTrapConfig) {
	defaultFloat(&trap.DepthMultiplier, 4.0)
	defaultFloat(&trap.MinDepth, 1.5)
	defaultFloat(&trap.MaxDepth, 6.0)
	defaultFloat(&trap.TpMultiplier, 2.5)
	defaultFloat(&trap.MinTP, 1.0)
	defaultFloat(&trap.MaxTP, 5.0)
	defaultFloat(&trap.SlMultiplier, 2.0)
	defaultFloat(&trap.MinSL, 1.0)
	defaultFloat(&trap.MaxSL, 4.0)
}

func setTrailingDynamicDefaults(trail *domain.TrailingConfig) {
	defaultFloat(&trail.ActivationMultiplier, 1.5)
	defaultFloat(&trail.MinActivation, 0.2)
	defaultFloat(&trail.MaxActivation, 3.0)
	defaultFloat(&trail.CallbackMultiplier, 0.7)
	defaultFloat(&trail.MinCallback, 0.3)
	defaultFloat(&trail.MaxCallback, 1.5)
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

func defaultStr(target *string, fallback string) {
	if *target == "" {
		*target = fallback
	}
}

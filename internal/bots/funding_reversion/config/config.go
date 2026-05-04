package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sysconfig "crypto-bot/internal/infrastructure/config"

	"github.com/tailscale/hujson"
)

// Load reads funding.json and returns the Config.
func Load(sysCfg *sysconfig.SystemConfig, fundingPath string) (*Config, error) {
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
			return fmt.Errorf("failed to parse trading defaults: %w", err)
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
	defaultFloat(&sc.MinFundingRate, d.MinFundingRate)
	defaultFloat(&sc.MaxPriceDiffPercent, d.MaxPriceDiffPercent)
	defaultInt(&sc.Leverage, d.Leverage)
	defaultStr((*string)(&sc.OpenType), string(d.OpenType))
	defaultStr((*string)(&sc.PositionMode), string(d.PositionMode))
	defaultFloat(&sc.TakeProfitPct, d.TakeProfitPct)
	defaultFloat(&sc.StopLossPct, d.StopLossPct)
	defaultFloat(&sc.TrapDepthPct, d.TrapDepthPct)
	defaultFloat(&sc.TrapTakeProfitPct, d.TrapTakeProfitPct)
	defaultFloat(&sc.TrapStopLossPct, d.TrapStopLossPct)

	if !sc.DynamicPricing.Enabled && d.DynamicPricing.Enabled {
		sc.DynamicPricing = d.DynamicPricing
	}
	if !sc.TrailingConfig.Enabled && d.TrailingConfig.Enabled {
		sc.TrailingConfig = d.TrailingConfig
	}
	if !sc.TrapTrailingConfig.Enabled && d.TrapTrailingConfig.Enabled {
		sc.TrapTrailingConfig = d.TrapTrailingConfig
	}

	if sc.EnableHedgeTrap == nil {
		sc.EnableHedgeTrap = d.EnableHedgeTrap
	}
}

func (c *Config) normalizeSymbolMetrics(sc *SymbolConfig) {
	sc.MinFundingRate /= 100
	sc.MaxPriceDiffPercent /= 100

	if sc.TakeProfitPct <= 0 {
		sc.TakeProfitPct = 20
	}
	if sc.StopLossPct <= 0 {
		sc.StopLossPct = 5
	}
	sc.TakeProfitPct /= 100
	sc.StopLossPct /= 100

	if sc.IsHedgeTrapEnabled() {
		if sc.TrapDepthPct <= 0 {
			sc.TrapDepthPct = 5
		}
		if sc.TrapTakeProfitPct <= 0 {
			sc.TrapTakeProfitPct = 2
		}
		if sc.TrapStopLossPct <= 0 {
			sc.TrapStopLossPct = 2
		}
		sc.TrapDepthPct /= 100
		sc.TrapTakeProfitPct /= 100
		sc.TrapStopLossPct /= 100
	}

	sc.TrailingConfig.ActivationPct /= 100
	sc.TrailingConfig.CallbackPct /= 100
	sc.TrapTrailingConfig.ActivationPct /= 100
	sc.TrapTrailingConfig.CallbackPct /= 100

	if sc.DynamicPricing.Enabled {
		setDynamicDefaults(&sc.DynamicPricing)
	}
}

func setDynamicDefaults(dp *DynamicPricingConfig) {
	defaultFloat(&dp.TrapDepthMultiplier, 4.0)
	defaultFloat(&dp.MinTrapDepth, 1.5)
	defaultFloat(&dp.MaxTrapDepth, 6.0)
	defaultFloat(&dp.TrapTpMultiplier, 2.5)
	defaultFloat(&dp.MinTrapTP, 1.0)
	defaultFloat(&dp.MaxTrapTP, 5.0)
	defaultFloat(&dp.TrapSlMultiplier, 2.0)
	defaultFloat(&dp.MinTrapSL, 1.0)
	defaultFloat(&dp.MaxTrapSL, 4.0)

	defaultFloat(&dp.TrailingActivationMultiplier, 1.5)
	defaultFloat(&dp.MinActivation, 0.2)
	defaultFloat(&dp.MaxActivation, 3.0)
	defaultFloat(&dp.TrailingCallbackMultiplier, 0.7)
	defaultFloat(&dp.MinCallback, 0.3)
	defaultFloat(&dp.MaxCallback, 1.5)
}

func (c *Config) defaultSymbolModes(sc *SymbolConfig) {
	switch strings.ToUpper(string(sc.OpenType)) {
	case "ISOLATED":
		sc.ParsedOpenType = 1
	case "CROSS":
		sc.ParsedOpenType = 2
	default:
		sc.ParsedOpenType = 1
	}

	switch strings.ToUpper(string(sc.PositionMode)) {
	case "HEDGE":
		sc.ParsedPositionMode = 1
	case "ONE_WAY":
		sc.ParsedPositionMode = 2
	default:
		sc.ParsedPositionMode = 1
	}
}

// IsHedgeTrapEnabled returns true if hedge trap is enabled for this symbol config.
func (sc *SymbolConfig) IsHedgeTrapEnabled() bool {
	return sc.EnableHedgeTrap != nil && *sc.EnableHedgeTrap
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

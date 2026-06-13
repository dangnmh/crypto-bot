package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/types"

	"github.com/go-playground/validator/v10"
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
		System:    sysCfg,
		Symbols:   symCfgs,
		Blacklist: &BlacklistConfig{},
	}

	configDir := filepath.Dir(fundingPath)
	blacklistPath := filepath.Join(configDir, "blacklist.jsonc")
	if blk, err := LoadBlacklist(blacklistPath); err == nil {
		cfg.Blacklist = blk
	} else if os.IsNotExist(err) {
		// Fallback to blacklist.json
		blacklistPathJSON := filepath.Join(configDir, "blacklist.json")
		if blk, err := LoadBlacklist(blacklistPathJSON); err == nil {
			cfg.Blacklist = blk
		}
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func LoadBlacklist(path string) (*BlacklistConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	data, err = hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("standardize blacklist config: %w", err)
	}

	var blacklist BlacklistConfig
	if err := json.Unmarshal(data, &blacklist); err != nil {
		return nil, fmt.Errorf("parse blacklist config: %w", err)
	}

	return &blacklist, nil
}

func (c *Config) parseTradingDefaults() (TradingDefaults, error) {
	var defaults TradingDefaults
	if c.System.TradingDefaults != nil {
		if err := json.Unmarshal(c.System.TradingDefaults, &defaults); err != nil {
			return defaults, fmt.Errorf("parse trading defaults: %w", err)
		}
	}
	return defaults, nil
}

func (c *Config) validateSymbols(defaults *TradingDefaults) error {
	for i := range c.Symbols {
		sc := &c.Symbols[i]

		sc.Exchange = strings.ToLower(strings.TrimSpace(sc.Exchange))
		if sc.Exchange == "" {
			return fmt.Errorf("symbols[%d].exchange is required", i)
		}
		if !c.exchangeConfigured(sc.Exchange) {
			return fmt.Errorf("symbols[%d].exchange %q is not configured", i, sc.Exchange)
		}

		c.applyDefaults(sc, defaults)
		c.normalizeSymbolMetrics(sc)
		c.defaultSymbolModes(sc)
	}
	return nil
}

func (c *Config) validate() error {
	defaults, err := c.parseTradingDefaults()
	if err != nil {
		return err
	}

	if err := c.validateSymbols(&defaults); err != nil {
		return err
	}

	validate := validator.New()
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})

	_ = validate.RegisterValidation("api_config", sysconfig.ValidateAPIConfigField)

	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

func (c *Config) exchangeConfigured(name string) bool {
	switch name {
	case exchange.ExchangeMexc:
		return c.System.ExchangeConfig.Mexc.Enable
	case exchange.ExchangeGate:
		return c.System.ExchangeConfig.Gate.Enable
	case exchange.ExchangeBybit:
		return c.System.ExchangeConfig.Bybit.Enable
	case exchange.ExchangeBinance:
		return c.System.ExchangeConfig.Binance.Enable
	case exchange.ExchangeOkx:
		return c.System.ExchangeConfig.Okx.Enable
	case exchange.ExchangeHyperliquid:
		return c.System.ExchangeConfig.Hyperliquid.Enable
	case exchange.ExchangeBitget:
		return c.System.ExchangeConfig.Bitget.Enable
	case exchange.ExchangeKucoin:
		return c.System.ExchangeConfig.Kucoin.Enable
	case exchange.ExchangeBingx:
		return c.System.ExchangeConfig.Bingx.Enable
	default:
		return false
	}
}

func (c *Config) applyDefaults(sc *SymbolConfig, d *TradingDefaults) {
	defaultFloat(&sc.MaxPriceDiffPercent, d.MaxPriceDiffPercent)
	defaultFloat(&sc.MinFundingRate, d.MinFundingRate)
	defaultInt(&sc.Leverage, d.Leverage)
	defaultStr((*string)(&sc.OpenType), d.OpenType)
	defaultStr((*string)(&sc.PositionMode), d.PositionMode)

	// 1. Resolve raw nested reversion config from defaults for the symbol's exchange
	exchName := sc.Exchange
	exchDefault := d.FundingReversion.Exchanges[exchName]

	if !sc.FundingReversion.Enabled && d.FundingReversion.Enabled {
		sc.FundingReversion.Enabled = true
		sc.FundingReversion.MaxLatency = d.FundingReversion.MaxLatency
		sc.FundingReversion.TakeProfitPct = exchDefault.TakeProfitPct
		sc.FundingReversion.StopLossPct = exchDefault.StopLossPct
		sc.FundingReversion.BufferTime = exchDefault.BufferTime
		sc.FundingReversion.PostSettleTimeout = exchDefault.PostSettleTimeout
	} else if sc.FundingReversion.Enabled {
		defaultDuration(&sc.FundingReversion.MaxLatency, d.FundingReversion.MaxLatency)
		defaultFloat(&sc.FundingReversion.TakeProfitPct, exchDefault.TakeProfitPct)
		defaultFloat(&sc.FundingReversion.StopLossPct, exchDefault.StopLossPct)
		defaultDuration(&sc.FundingReversion.BufferTime, exchDefault.BufferTime)
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

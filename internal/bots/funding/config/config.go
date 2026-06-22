package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/types"

	"github.com/go-playground/validator/v10"
)

// Load reads funding.json and returns the Config.
func Load(sysCfg *SystemConfig, fundingPath string) (*Config, error) {
	cfg := &Config{
		System:    sysCfg,
		Symbols:   nil,
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

	reversionCfg, err := loadReversionConfig(configDir)
	if err != nil {
		return nil, err
	}
	cfg.Reversion = reversionCfg

	// Normalize Safety limit percentage
	cfg.Reversion.Safety.MaxImpactRatio /= 100

	if cfg.Reversion.Scanners.Configured {
		symCfgs, err := loadSymbolsConfig(fundingPath)
		if err != nil {
			return nil, err
		}
		cfg.Symbols = symCfgs
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func loadSymbolsConfig(fundingPath string) ([]SymbolConfig, error) {
	symCfgs, err := pkgconfig.Load[[]SymbolConfig](fundingPath)
	if err != nil {
		if strings.Contains(err.Error(), "read config") {
			return nil, fmt.Errorf("read funding config %s: %w", fundingPath, err)
		}
		return nil, fmt.Errorf("parse funding config: %w", err)
	}
	return *symCfgs, nil
}

func loadReversionConfig(configDir string) (*ReversionConfig, error) {
	reversionPath := filepath.Join(configDir, "reversion.jsonc")
	reversionCfg, err := pkgconfig.Load[ReversionConfig](reversionPath)
	if err != nil {
		if strings.Contains(err.Error(), "read config") {
			return nil, fmt.Errorf("read reversion config %s: %w", reversionPath, err)
		}
		return nil, fmt.Errorf("parse reversion config: %w", err)
	}

	if reversionCfg.Sync.FundingSync <= 0 {
		reversionCfg.Sync.FundingSync = types.Duration(30 * time.Second)
	}
	if reversionCfg.Sync.Time <= 0 {
		reversionCfg.Sync.Time = types.Duration(30 * time.Second)
	}
	if reversionCfg.Sync.Ticker <= 0 {
		reversionCfg.Sync.Ticker = types.Duration(30 * time.Second)
	}
	if reversionCfg.Sync.Contract <= 0 {
		reversionCfg.Sync.Contract = types.Duration(300 * time.Second)
	}

	reversionCfg.TradeSide = strings.ToLower(strings.TrimSpace(reversionCfg.TradeSide))
	if reversionCfg.TradeSide == "" {
		reversionCfg.TradeSide = "both"
	}

	return reversionCfg, nil
}

func LoadBlacklist(path string) (*BlacklistConfig, error) {
	return pkgconfig.Load[BlacklistConfig](path)
}

func (c *Config) parseTradingDefaults() (RawFundingReversionConfig, error) {
	return c.Reversion.RawFundingReversionConfig, nil
}

func (c *Config) validateSymbols(defaults *RawFundingReversionConfig) error {
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

func (c *Config) applyDefaults(sc *SymbolConfig, d *RawFundingReversionConfig) {
	// Merge exchange-specific configs with strategy defaults.
	exchName := sc.Exchange
	exchConfig := d.Default // Start with defaults

	// Override with exchange-specific settings if present
	if specific, exists := d.Exchanges[exchName]; exists {
		if specific.TakeProfitPct > 0 {
			exchConfig.TakeProfitPct = specific.TakeProfitPct
		}
		if specific.StopLossPct > 0 {
			exchConfig.StopLossPct = specific.StopLossPct
		}
		if specific.BufferTime != 0 {
			exchConfig.BufferTime = specific.BufferTime
		}
		if specific.PostSettleTimeout != 0 {
			exchConfig.PostSettleTimeout = specific.PostSettleTimeout
		}
		if specific.Leverage > 0 {
			exchConfig.Leverage = specific.Leverage
		}
		if specific.MarginUSD > 0 {
			exchConfig.MarginUSD = specific.MarginUSD
		}
		if specific.MinVol24USD > 0 {
			exchConfig.MinVol24USD = specific.MinVol24USD
		}
		if specific.MinFundingRate > 0 {
			exchConfig.MinFundingRate = specific.MinFundingRate
		}
	}

	defaultFloat(&sc.MaxPriceDiffPercent, c.Reversion.Safety.MaxPriceDiffPercent)
	defaultFloat(&sc.MinFundingRate, exchConfig.MinFundingRate)
	defaultFloat(&sc.MinVol24USD, exchConfig.MinVol24USD)

	// Apply leverage and margin (either exchange-specific, or default fallback)
	defaultInt(&sc.Leverage, exchConfig.Leverage)
	defaultFloat(&sc.MarginUSDT, exchConfig.MarginUSD)

	defaultStr((*string)(&sc.OpenType), d.OpenType)
	defaultStr((*string)(&sc.PositionMode), d.PositionMode)

	if !sc.FundingReversion.Enabled && d.Enabled {
		sc.FundingReversion.Enabled = true
		sc.FundingReversion.MaxLatency = c.Reversion.Safety.MaxLatency
		sc.FundingReversion.TakeProfitPct = exchConfig.TakeProfitPct
		sc.FundingReversion.StopLossPct = exchConfig.StopLossPct
		sc.FundingReversion.BufferTime = exchConfig.BufferTime
		sc.FundingReversion.PostSettleTimeout = exchConfig.PostSettleTimeout
	} else if sc.FundingReversion.Enabled {
		defaultDuration(&sc.FundingReversion.MaxLatency, c.Reversion.Safety.MaxLatency)
		defaultFloat(&sc.FundingReversion.TakeProfitPct, exchConfig.TakeProfitPct)
		defaultFloat(&sc.FundingReversion.StopLossPct, exchConfig.StopLossPct)
		defaultDuration(&sc.FundingReversion.BufferTime, exchConfig.BufferTime)
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

func (c *Config) NewSymbolConfig(exchangeName, symbol string) (SymbolConfig, error) {
	defaults, err := c.parseTradingDefaults()
	if err != nil {
		return SymbolConfig{}, err
	}

	sc := SymbolConfig{
		Symbol:   symbol,
		Exchange: exchangeName,
	}

	c.applyDefaults(&sc, &defaults)
	c.normalizeSymbolMetrics(&sc)
	c.defaultSymbolModes(&sc)

	return sc, nil
}

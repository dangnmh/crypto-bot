package config

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/types"

	"github.com/go-playground/validator/v10"
)

// LoadAndValidate reads a config file and validates it, supporting both structs and slices.
func LoadAndValidate[T any](path string) (*T, error) {
	cfg, err := pkgconfig.Load[T](path)
	if err != nil {
		return nil, err
	}

	validate := newValidator()

	val := reflect.ValueOf(cfg)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	typeName := reflect.TypeFor[T]().Name()
	if typeName != "SystemConfig" && typeName != "FundingConfig" && typeName != "ReversionConfig" {
		if val.Kind() == reflect.Struct {
			if err := validate.Struct(cfg); err != nil {
				return nil, fmt.Errorf("validation failed: %w", err)
			}
		} else if val.Kind() == reflect.Slice {
			for i := 0; i < val.Len(); i++ {
				item := val.Index(i).Interface()
				if err := validate.Struct(item); err != nil {
					return nil, fmt.Errorf("validation failed at index %d: %w", i, err)
				}
			}
		}
	}

	return cfg, nil
}

// Load reads configuration files using specific paths and returns the Config.
func Load(sysCfg *SystemConfig, fundingPath, blacklistPath, reversionPath, obfuscatorPath, dilutionPath string) (*Config, error) {
	cfg := &Config{
		System:    sysCfg,
		Symbols:   nil,
		Blacklist: &BlacklistConfig{},
	}

	blk, err := LoadAndValidate[BlacklistConfig](blacklistPath)
	if err != nil {
		return nil, fmt.Errorf("parse blacklist config: %w", err)
	}
	cfg.Blacklist = blk

	reversionCfg, err := LoadAndValidate[ReversionConfig](reversionPath)
	if err != nil {
		return nil, fmt.Errorf("parse reversion config: %w", err)
	}
	cfg.Reversion = reversionCfg

	obf, err := LoadAndValidate[ObfuscatorConfig](obfuscatorPath)
	if err != nil {
		return nil, fmt.Errorf("parse obfuscator config: %w", err)
	}
	cfg.Obfuscator = obf

	dilutionCfg, err := LoadAndValidate[DilutionConfig](dilutionPath)
	if err != nil {
		return nil, fmt.Errorf("parse dilution config: %w", err)
	}
	cfg.Dilution = dilutionCfg

	applyReversionDefaults(cfg.Reversion)

	if cfg.Reversion.Scanners.Configured {
		symCfgs, err := LoadAndValidate[FundingConfig](fundingPath)
		if err != nil {
			return nil, fmt.Errorf("parse funding config: %w", err)
		}
		cfg.Symbols = []SymbolConfig(*symCfgs)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func applyReversionDefaults(r *ReversionConfig) {
	if r == nil {
		return
	}
	if r.Default.MaxCandidateTrade <= 0 {
		r.Default.MaxCandidateTrade = 1
	}
	for name := range r.Exchanges {
		exch := r.Exchanges[name]
		if exch.MaxCandidateTrade <= 0 {
			exch.MaxCandidateTrade = r.Default.MaxCandidateTrade
			r.Exchanges[name] = exch
		}
	}

	if r.Sync.FundingSync <= 0 {
		r.Sync.FundingSync = types.Duration(30 * time.Second)
	}
	if r.Sync.Time <= 0 {
		r.Sync.Time = types.Duration(30 * time.Second)
	}
	if r.Sync.Ticker <= 0 {
		r.Sync.Ticker = types.Duration(30 * time.Second)
	}
	if r.Sync.Contract <= 0 {
		r.Sync.Contract = types.Duration(300 * time.Second)
	}

	r.TradeSide = strings.ToLower(strings.TrimSpace(r.TradeSide))
	if r.TradeSide == "" {
		r.TradeSide = "both"
	}

	// Normalize Safety limit percentage
	r.Safety.MaxImpactRatio /= 100
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

	validate := newValidator()

	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

func (c *Config) exchangeConfigured(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if cfg, ok := c.System.ExchangeConfig[name]; ok {
		return cfg.IsEnabled()
	}
	baseName := strings.TrimSuffix(strings.TrimSuffix(name, "_spot"), "_futures")
	apiCfg, ok := c.System.ExchangeConfig[baseName]
	if !ok {
		return false
	}
	if strings.HasSuffix(name, "_futures") {
		return apiCfg.Future != nil && apiCfg.Future.Enable
	}
	if strings.HasSuffix(name, "_spot") {
		return apiCfg.Spot != nil && apiCfg.Spot.Enable
	}
	return apiCfg.IsEnabled()
}

func newValidator() *validator.Validate {
	validate := validator.New()
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	_ = validate.RegisterValidation("api_config", sysconfig.ValidateAPIConfigField)
	_ = validate.RegisterValidation("supported_exchange", func(fl validator.FieldLevel) bool {
		return sysconfig.IsSupportedExchange(fl.Field().String())
	})
	return validate
}

func MergeExchangeReversionConfig(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	mergeReversionCore(dest, src)
	mergeRiskAndScoring(dest, src)
	mergePnLTrailing(dest, src)
	mergeDynamicTP(dest, src)
}

func mergeReversionCore(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.TakeProfitPct > 0 {
		dest.TakeProfitPct = src.TakeProfitPct
	}
	if src.StopLossPct > 0 {
		dest.StopLossPct = src.StopLossPct
	}
	if src.BufferTime != 0 {
		dest.BufferTime = src.BufferTime
	}
	if src.PostSettleTimeout != 0 {
		dest.PostSettleTimeout = src.PostSettleTimeout
	}
	if src.Leverage > 0 {
		dest.Leverage = src.Leverage
	}
	if src.MarginUSD > 0 {
		dest.MarginUSD = src.MarginUSD
	}
	if src.MinVol24USD > 0 {
		dest.MinVol24USD = src.MinVol24USD
	}
	if src.MinFundingRate > 0 {
		dest.MinFundingRate = src.MinFundingRate
	}
}

func mergeRiskAndScoring(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.MaxCandidateTrade > 0 {
		dest.MaxCandidateTrade = src.MaxCandidateTrade
	}
	if src.MaxMarginUSDOfCandidate > 0 {
		dest.MaxMarginUSDOfCandidate = src.MaxMarginUSDOfCandidate
	}
	if src.ScoringRateWeight > 0 {
		dest.ScoringRateWeight = src.ScoringRateWeight
	}
	if src.ScoringVolumeWeight > 0 {
		dest.ScoringVolumeWeight = src.ScoringVolumeWeight
	}
	if src.MaxVolumeScore > 0 {
		dest.MaxVolumeScore = src.MaxVolumeScore
	}
}

func mergePnLTrailing(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.PnLTrailing.Enabled {
		dest.PnLTrailing.Enabled = src.PnLTrailing.Enabled
	}
	if src.PnLTrailing.DropPct > 0 {
		dest.PnLTrailing.DropPct = src.PnLTrailing.DropPct
	}
	if src.PnLTrailing.ConfirmTicks > 0 {
		dest.PnLTrailing.ConfirmTicks = src.PnLTrailing.ConfirmTicks
	}
}

func mergeDynamicTP(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.DynamicTP.Enabled {
		dest.DynamicTP.Enabled = src.DynamicTP.Enabled
	}
	if src.DynamicTP.TPMultiplier > 0 {
		dest.DynamicTP.TPMultiplier = src.DynamicTP.TPMultiplier
	}
	if src.DynamicTP.MinTakeProfitPct > 0 {
		dest.DynamicTP.MinTakeProfitPct = src.DynamicTP.MinTakeProfitPct
	}
	if src.DynamicTP.MaxTakeProfitPct > 0 {
		dest.DynamicTP.MaxTakeProfitPct = src.DynamicTP.MaxTakeProfitPct
	}
}

func (c *Config) applyDefaults(sc *SymbolConfig, d *RawFundingReversionConfig) {
	// Merge exchange-specific configs with strategy defaults.
	exchName := sc.Exchange
	exchConfig := d.Default // Start with defaults

	// Override with exchange-specific settings if present
	if specific, exists := d.Exchanges[exchName]; exists {
		MergeExchangeReversionConfig(&exchConfig, specific)
	}

	if sc.MaxPriceDiffPercent == 0 {
		sc.MaxPriceDiffPercent = c.Reversion.Safety.MaxPriceDiffPercent
	}
	if sc.MinFundingRate == 0 {
		sc.MinFundingRate = exchConfig.MinFundingRate
	}
	if sc.MinVol24USD == 0 {
		sc.MinVol24USD = exchConfig.MinVol24USD
	}

	// Apply leverage and margin (either exchange-specific, or default fallback)
	if sc.Leverage == 0 {
		sc.Leverage = exchConfig.Leverage
	}
	if sc.MarginUSDT == 0 {
		sc.MarginUSDT = exchConfig.MarginUSD
	}

	if sc.OpenType == "" {
		sc.OpenType = OpenType(d.OpenType)
	}
	if sc.PositionMode == "" {
		sc.PositionMode = PositionMode(d.PositionMode)
	}

	c.mergeFundingReversion(sc, d, &exchConfig)
}

func (c *Config) mergeFundingReversion(sc *SymbolConfig, d *RawFundingReversionConfig, exchConfig *ExchangeReversionConfig) {
	if !sc.FundingReversion.Enabled && d.Enabled {
		sc.FundingReversion.Enabled = true
		sc.FundingReversion.MaxLatency = c.Reversion.Safety.MaxLatency
		sc.FundingReversion.TakeProfitPct = exchConfig.TakeProfitPct
		sc.FundingReversion.StopLossPct = exchConfig.StopLossPct
		sc.FundingReversion.BufferTime = exchConfig.BufferTime
		sc.FundingReversion.PostSettleTimeout = exchConfig.PostSettleTimeout
		sc.FundingReversion.PnLTrailing = exchConfig.PnLTrailing
		sc.FundingReversion.DynamicTP = exchConfig.DynamicTP
	} else if sc.FundingReversion.Enabled {
		if sc.FundingReversion.MaxLatency == 0 {
			sc.FundingReversion.MaxLatency = c.Reversion.Safety.MaxLatency
		}
		if sc.FundingReversion.TakeProfitPct == 0 {
			sc.FundingReversion.TakeProfitPct = exchConfig.TakeProfitPct
		}
		if sc.FundingReversion.StopLossPct == 0 {
			sc.FundingReversion.StopLossPct = exchConfig.StopLossPct
		}
		if sc.FundingReversion.BufferTime == 0 {
			sc.FundingReversion.BufferTime = exchConfig.BufferTime
		}
		mergeSymbolPnLTrailing(sc, exchConfig)
		mergeSymbolDynamicTP(sc, exchConfig)
	}
}

func mergeSymbolPnLTrailing(sc *SymbolConfig, exchConfig *ExchangeReversionConfig) {
	if !sc.FundingReversion.PnLTrailing.Enabled && exchConfig.PnLTrailing.Enabled {
		sc.FundingReversion.PnLTrailing.Enabled = exchConfig.PnLTrailing.Enabled
	}
	if sc.FundingReversion.PnLTrailing.DropPct == 0 {
		sc.FundingReversion.PnLTrailing.DropPct = exchConfig.PnLTrailing.DropPct
	}
	if sc.FundingReversion.PnLTrailing.ConfirmTicks == 0 {
		sc.FundingReversion.PnLTrailing.ConfirmTicks = exchConfig.PnLTrailing.ConfirmTicks
	}
}

func mergeSymbolDynamicTP(sc *SymbolConfig, exchConfig *ExchangeReversionConfig) {
	if !sc.FundingReversion.DynamicTP.Enabled && exchConfig.DynamicTP.Enabled {
		sc.FundingReversion.DynamicTP.Enabled = exchConfig.DynamicTP.Enabled
	}
	if sc.FundingReversion.DynamicTP.TPMultiplier == 0 {
		sc.FundingReversion.DynamicTP.TPMultiplier = exchConfig.DynamicTP.TPMultiplier
	}
	if sc.FundingReversion.DynamicTP.MinTakeProfitPct == 0 {
		sc.FundingReversion.DynamicTP.MinTakeProfitPct = exchConfig.DynamicTP.MinTakeProfitPct
	}
	if sc.FundingReversion.DynamicTP.MaxTakeProfitPct == 0 {
		sc.FundingReversion.DynamicTP.MaxTakeProfitPct = exchConfig.DynamicTP.MaxTakeProfitPct
	}
}

func (c *Config) normalizeSymbolMetrics(sc *SymbolConfig) {
	sc.MinFundingRate = normalizeFundingRateThreshold(sc.MinFundingRate)

	if sc.FundingReversion.Enabled {
		if sc.FundingReversion.MaxLatency == 0 {
			sc.FundingReversion.MaxLatency = types.Duration(200 * time.Millisecond)
		}
		if sc.FundingReversion.BufferTime == 0 {
			sc.FundingReversion.BufferTime = types.Duration(10 * time.Millisecond)
		}
		if sc.FundingReversion.PostSettleTimeout == 0 {
			sc.FundingReversion.PostSettleTimeout = types.Duration(60 * time.Second)
		}

		if sc.FundingReversion.TakeProfitPct <= 0 {
			sc.FundingReversion.TakeProfitPct = 20
		}
		if sc.FundingReversion.StopLossPct <= 0 {
			sc.FundingReversion.StopLossPct = 5
		}
		sc.FundingReversion.TakeProfitPct = normalizePercentRatio(sc.FundingReversion.TakeProfitPct)
		sc.FundingReversion.StopLossPct = normalizePercentRatio(sc.FundingReversion.StopLossPct)
		if sc.FundingReversion.DynamicTP.MinTakeProfitPct > 0 {
			sc.FundingReversion.DynamicTP.MinTakeProfitPct = normalizePercentRatio(sc.FundingReversion.DynamicTP.MinTakeProfitPct)
		}
		if sc.FundingReversion.DynamicTP.MaxTakeProfitPct > 0 {
			sc.FundingReversion.DynamicTP.MaxTakeProfitPct = normalizePercentRatio(sc.FundingReversion.DynamicTP.MaxTakeProfitPct)
		}
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

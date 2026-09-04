package config

import (
	"strings"

	"crypto-bot/internal/bots/funding/domain"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/types"
)

// OpenType specifies the margin mode for a position.
type OpenType string

const (
	OpenTypeIsolated OpenType = "ISOLATED"
	OpenTypeCross    OpenType = "CROSS"
)

// PositionMode specifies whether the position is one-way or hedge.
type PositionMode string

const (
	PositionModeHedge  PositionMode = "HEDGE"
	PositionModeOneWay PositionMode = "ONE_WAY"
)

// SymbolConfig represents per-symbol trading settings loaded from funding.json.
type SymbolConfig struct {
	Symbol              string       `json:"symbol" validate:"required"`
	Exchange            string       `json:"exchange" validate:"required,supported_exchange"`
	SimulateSettle      string       `json:"simulateSettle"`
	MaxPriceDiffPercent float64      `json:"maxPriceDiffPercent"`
	MarginUSDT          float64      `json:"marginUSDT" validate:"gt=0"`
	Leverage            int          `json:"leverage" validate:"gte=1"`
	OpenType            OpenType     `json:"openType"`
	PositionMode        PositionMode `json:"positionMode"`
	MinFundingRate      float64      `json:"minFundingRate"`
	MinVol24USD         float64      `json:"minVol24USD"`
	// Reuses domain types directly — single source of truth.
	FundingReversion domain.FundingReversionConfig `json:"fundingReversion"`

	ParsedOpenType     int `json:"-"`
	ParsedPositionMode int `json:"-"`
}

// SyncConfig holds intervals for background sync tasks.
type SyncConfig struct {
	sysconfig.SyncConfig
	// Funding Sync Interval
	FundingSync types.Duration `json:"funding"`
}

type ReversionNotifierConfig struct {
	Enabled bool `json:"enable"`
}

type StatsReporterConfig struct {
	Enabled bool `json:"enable"`
}

type PriceTrackerConfig struct {
	Enabled bool `json:"enable"`
}

type ObfuscatorConfig struct {
	Enabled        bool                              `json:"enabled"`
	PollInterval   types.Duration                    `json:"pollInterval" validate:"required"`
	Jitter         types.Duration                    `json:"jitter,omitempty"`
	LookbackWindow types.Duration                    `json:"lookbackWindow" validate:"required"`
	Exchanges      map[string]ExchangeObfuscationCfg `json:"exchanges" validate:"dive"`
}

type ExchangeObfuscationCfg struct {
	Enabled             bool    `json:"enabled"`
	NetPnLThresholdUSDT float64 `json:"netPnLThresholdUSDT" validate:"gte=0"`
	MinNotionalUSD      float64 `json:"minNotionalUSD" validate:"gt=0"`
	MaxNotionalUSD      float64 `json:"maxNotionalUSD" validate:"gt=0,gtefield=MinNotionalUSD"`
	MarginUSDT          float64 `json:"marginUSDT" validate:"gt=0"`
	Leverage            int     `json:"leverage" validate:"gte=1"`
	TakeProfitPct       float64 `json:"takeProfitPct" validate:"gt=0"`
	StopLossPct         float64 `json:"stopLossPct" validate:"gt=0"`
	MaxPriceDiffPercent float64 `json:"maxPriceDiffPercent,omitempty" validate:"omitempty,gt=0"`
	MinHoldSec          int     `json:"minHoldSec" validate:"gt=0"`
	MaxHoldSec          int     `json:"maxHoldSec" validate:"gt=0,gtefield=MinHoldSec"`
	MaxActiveOrders     int     `json:"maxActiveOrders" validate:"gt=0"`
	SacrificeLossPct    float64 `json:"sacrificeLossPct" validate:"gt=0,lte=100"`
	MaxDailyLossUSD     float64 `json:"maxDailyLossUSD" validate:"gt=0"`
}

// OrderNotionalUSD computes the notional order value in USD based on MarginUSDT and Leverage,
// clamped between MinNotionalUSD and MaxNotionalUSD.
func (c ExchangeObfuscationCfg) OrderNotionalUSD() float64 {
	baseNotional := c.MarginUSDT * float64(c.Leverage)
	if c.MinNotionalUSD > 0 && baseNotional < c.MinNotionalUSD {
		baseNotional = c.MinNotionalUSD
	}
	if c.MaxNotionalUSD > 0 && baseNotional > c.MaxNotionalUSD {
		baseNotional = c.MaxNotionalUSD
	}
	return baseNotional
}

type DilutionConfig struct {
	Enabled      bool                           `json:"enabled"`
	PollInterval types.Duration                 `json:"pollInterval" validate:"required"`
	Jitter       types.Duration                 `json:"jitter,omitempty"`
	Exchanges    map[string]ExchangeDilutionCfg `json:"exchanges" validate:"dive"`
}

type ExchangeDilutionCfg struct {
	Enabled               bool           `json:"enabled"`
	Symbol                string         `json:"symbol" validate:"required"`
	MaxPositionUSD        float64        `json:"maxPositionUSD" validate:"gt=0"`
	Leverage              int            `json:"leverage" validate:"gte=1"`
	MarginUSD             float64        `json:"marginUSD" validate:"gt=0"`
	UnfilledCancelTimeout types.Duration `json:"unfilledCancelTimeout,omitempty"`
	PositionCloseTimeout  types.Duration `json:"positionCloseTimeout" validate:"required"`
	TakeProfitPct         float64        `json:"takeProfitPct,omitempty" validate:"omitempty,gt=0"`
	StopLossPct           float64        `json:"stopLossPct,omitempty" validate:"omitempty,gt=0"`
	SpreadOffsetTicks     int            `json:"spreadOffsetTicks" validate:"gte=0"`
}

// OrderNotionalUSD computes the notional order value in USD based on MarginUSD and Leverage,
// capped at MaxPositionUSD if specified.
func (c ExchangeDilutionCfg) OrderNotionalUSD() float64 {
	notional := c.MarginUSD * float64(c.Leverage)
	if c.MaxPositionUSD > 0 && notional > c.MaxPositionUSD {
		return c.MaxPositionUSD
	}
	return notional
}

type ReversionConfig struct {
	RawFundingReversionConfig
	Sync          SyncConfig              `json:"sync"`
	Safety        SafetyConfig            `json:"safety"`
	Scanners      ScannersConfig          `json:"scanners"`
	Notifier      ReversionNotifierConfig `json:"notifier"`
	StatsReporter StatsReporterConfig     `json:"statsReporter"`
	PriceTracker  PriceTrackerConfig      `json:"priceTracker"`
}

// FundingConfig represents the array of symbol configurations loaded from funding.jsonc.
// FundingConfig represents the array of symbol configurations loaded from funding.jsonc.
type FundingConfig []SymbolConfig

// Config is the root configuration containing System, Symbols, Blacklist, Reversion, Obfuscator, and Dilution configs.
type Config struct {
	System     *SystemConfig     `json:"-" validate:"required"`
	Symbols    []SymbolConfig    `json:"-" validate:"dive"`
	Blacklist  *BlacklistConfig  `json:"-"`
	Reversion  *ReversionConfig  `json:"-" validate:"required"`
	Obfuscator *ObfuscatorConfig `json:"-" validate:"omitempty"`
	Dilution   *DilutionConfig   `json:"-" validate:"omitempty"`
}

type RawFundingReversionConfig struct {
	Enabled      bool                               `json:"enabled"`
	OpenType     string                             `json:"openType"`
	PositionMode string                             `json:"positionMode"`
	TradeSide    string                             `json:"tradeSide" validate:"omitempty,oneof=long short both"`
	Default      ExchangeReversionConfig            `json:"default"`
	Exchanges    map[string]ExchangeReversionConfig `json:"exchanges"`
}

type ExchangeReversionConfig struct {
	TakeProfitPct           float64           `json:"takeProfitPct" validate:"omitempty,gt=0"`
	StopLossPct             float64           `json:"stopLossPct" validate:"omitempty,gt=0"`
	BufferTime              types.Duration    `json:"bufferTime"`
	PostSettleTimeout       types.Duration    `json:"postSettleTimeout"`
	Leverage                int               `json:"leverage" validate:"omitempty,gt=0"`
	MarginUSD               float64           `json:"marginUSD" validate:"omitempty,gt=0"`
	MinVol24USD             float64           `json:"minVol24USD" validate:"omitempty,gte=0"`
	MinFundingRate          float64           `json:"minFundingRate" validate:"omitempty,gte=0"`
	MaxCandidateTrade       int               `json:"maxCandidateTrade" validate:"omitempty,gt=0"`
	MaxMarginUSDOfCandidate float64           `json:"maxMarginUSDOfCandidate,omitempty" validate:"omitempty,gt=0"`
	ScoringRateWeight       float64           `json:"scoringRateWeight,omitempty" validate:"omitempty,gt=0"`
	ScoringVolumeWeight     float64           `json:"scoringVolumeWeight,omitempty" validate:"omitempty,gt=0"`
	MaxVolumeScore          float64           `json:"maxVolumeScore,omitempty" validate:"omitempty,gt=0"`
	PnLTrailing             PnLTrailingConfig `json:"pnlTrailing"`
	DynamicTP               DynamicTPConfig   `json:"dynamicTP"`
}

type PnLTrailingConfig = domain.PnLTrailingConfig
type DynamicTPConfig = domain.DynamicTPConfig

type BlacklistConfig map[string][]string

func (b BlacklistConfig) GetCommonBlacklist() []string {
	if b == nil {
		return nil
	}
	return b["common"]
}

func (b BlacklistConfig) GetExchangeBlacklist(exchange string) []string {
	if b == nil {
		return nil
	}
	return b[strings.ToLower(strings.TrimSpace(exchange))]
}

func (b BlacklistConfig) IsBlacklisted(exchange, symbol string) bool {
	if b == nil {
		return false
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	// Check common blacklist
	for _, s := range b["common"] {
		if strings.ToUpper(strings.TrimSpace(s)) == sym {
			return true
		}
	}
	// Check exchange specific blacklist
	exchList := b.GetExchangeBlacklist(exchange)
	for _, s := range exchList {
		if strings.ToUpper(strings.TrimSpace(s)) == sym {
			return true
		}
	}
	return false
}

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

type ReversionConfig struct {
	RawFundingReversionConfig
	Sync     SyncConfig              `json:"sync"`
	Safety   SafetyConfig            `json:"safety"`
	Scanners ScannersConfig          `json:"scanners"`
	Notifier ReversionNotifierConfig `json:"notifier"`
}

// FundingConfig represents the array of symbol configurations loaded from funding.jsonc.
type FundingConfig []SymbolConfig

// Config is the root configuration containing both System and Funding configs.
type Config struct {
	System    *SystemConfig    `json:"-" validate:"required"`
	Symbols   []SymbolConfig   `json:"-" validate:"dive"`
	Blacklist *BlacklistConfig `json:"-"`
	Reversion *ReversionConfig `json:"-" validate:"required"`
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
	TakeProfitPct     float64        `json:"takeProfitPct"`
	StopLossPct       float64        `json:"stopLossPct"`
	BufferTime        types.Duration `json:"bufferTime"`
	PostSettleTimeout types.Duration `json:"postSettleTimeout"`
	Leverage          int            `json:"leverage"`
	MarginUSD         float64        `json:"marginUSD"`
	MinVol24USD       float64        `json:"minVol24USD"`
	MinFundingRate    float64        `json:"minFundingRate"`
	MaxCandidateTrade int            `json:"maxCandidateTrade"`
}

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

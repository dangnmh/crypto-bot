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
	Exchange            string       `json:"exchange" validate:"required,oneof=mexc gate bybit binance okx hyperliquid bitget kucoin bingx"`
	SimulateSettle      string       `json:"simulateSettle"`
	MaxPriceDiffPercent float64      `json:"maxPriceDiffPercent"`
	MarginUSDT          float64      `json:"marginUSDT" validate:"gt=0"`
	Leverage            int          `json:"leverage" validate:"gte=1"`
	OpenType            OpenType     `json:"openType"`
	PositionMode        PositionMode `json:"positionMode"`
	MinFundingRate      float64      `json:"minFundingRate"`
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

// Config is the root configuration containing both System and Funding configs.
type Config struct {
	System    *SystemConfig    `json:"-" validate:"required"`
	Symbols   []SymbolConfig   `json:"-" validate:"dive"`
	Blacklist *BlacklistConfig `json:"-"`
	Reversion *ReversionConfig `json:"-" validate:"required"`
}

type RawFundingReversionConfig struct {
	Enabled             bool                               `json:"enabled"`
	MaxLatency          types.Duration                     `json:"maxLatency"`
	MinFundingRate      float64                            `json:"minFundingRate"`
	MaxPriceDiffPercent float64                            `json:"maxPriceDiffPercent"`
	OpenType            string                             `json:"openType"`
	PositionMode        string                             `json:"positionMode"`
	Default             ExchangeReversionConfig            `json:"default"`
	Exchanges           map[string]ExchangeReversionConfig `json:"exchanges"`
}

type ExchangeReversionConfig struct {
	TakeProfitPct     float64        `json:"takeProfitPct"`
	StopLossPct       float64        `json:"stopLossPct"`
	BufferTime        types.Duration `json:"bufferTime"`
	PostSettleTimeout types.Duration `json:"postSettleTimeout"`
	Leverage          int            `json:"leverage"`
	MarginUSD         float64        `json:"marginUSD"`
}

type BlacklistConfig struct {
	Common      []string `json:"common"`
	Mexc        []string `json:"mexc"`
	Gate        []string `json:"gate"`
	Bybit       []string `json:"bybit"`
	Binance     []string `json:"binance"`
	Okx         []string `json:"okx"`
	Hyperliquid []string `json:"hyperliquid"`
	Bitget      []string `json:"bitget"`
	Kucoin      []string `json:"kucoin"`
	Bingx       []string `json:"bingx"`
}

func (b *BlacklistConfig) GetExchangeBlacklist(exchange string) []string {
	if b == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(exchange)) {
	case "mexc":
		return b.Mexc
	case "gate":
		return b.Gate
	case "bybit":
		return b.Bybit
	case "binance":
		return b.Binance
	case "okx":
		return b.Okx
	case "hyperliquid":
		return b.Hyperliquid
	case "bitget":
		return b.Bitget
	case "kucoin":
		return b.Kucoin
	case "bingx":
		return b.Bingx
	default:
		return nil
	}
}

func (b *BlacklistConfig) IsBlacklisted(exchange, symbol string) bool {
	if b == nil {
		return false
	}
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	// Check common blacklist
	for _, s := range b.Common {
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

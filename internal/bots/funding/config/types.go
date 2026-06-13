package config

import (
	"strings"

	"crypto-bot/internal/bots/funding/domain"
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

// Config is the root configuration containing both System and Funding configs.
type Config struct {
	System    *SystemConfig    `validate:"required"`
	Symbols   []SymbolConfig   `json:"symbols" validate:"required,gt=0,dive"`
	Blacklist *BlacklistConfig `json:"-"`
}

type RawFundingReversionConfig struct {
	Enabled    bool                               `json:"enabled"`
	MaxLatency types.Duration                     `json:"maxLatency"`
	Exchanges  map[string]ExchangeReversionConfig `json:"exchanges"`
}

type ExchangeReversionConfig struct {
	TakeProfitPct     float64        `json:"takeProfitPct"`
	StopLossPct       float64        `json:"stopLossPct"`
	BufferTime        types.Duration `json:"bufferTime"`
	PostSettleTimeout types.Duration `json:"postSettleTimeout"`
}

// TradingDefaults is a temporary parsing struct to extract the opaque tradingDefaults
// block from system config and merge into per-symbol configs.
type TradingDefaults struct {
	MinFundingRate      float64 `json:"minFundingRate"`
	MaxPriceDiffPercent float64 `json:"maxPriceDiffPercent"`
	Leverage            int     `json:"leverage"`
	OpenType            string  `json:"openType"`
	PositionMode        string  `json:"positionMode"`

	// Raw parsed defaults
	FundingReversion RawFundingReversionConfig `json:"fundingReversion"`
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
	var exchList []string
	switch strings.ToLower(exchange) {
	case "mexc":
		exchList = b.Mexc
	case "gate":
		exchList = b.Gate
	case "bybit":
		exchList = b.Bybit
	case "binance":
		exchList = b.Binance
	case "okx":
		exchList = b.Okx
	case "hyperliquid":
		exchList = b.Hyperliquid
	case "bitget":
		exchList = b.Bitget
	case "kucoin":
		exchList = b.Kucoin
	case "bingx":
		exchList = b.Bingx
	}
	for _, s := range exchList {
		if strings.ToUpper(strings.TrimSpace(s)) == sym {
			return true
		}
	}
	return false
}

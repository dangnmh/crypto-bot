package config

import "crypto-bot/internal/bots/funding_reversion/domain"

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
	Symbol              string       `json:"symbol"`
	SimulateSettle      string       `json:"simulateSettle"`
	MaxPriceDiffPercent float64      `json:"maxPriceDiffPercent"`
	MarginUSDT          float64      `json:"marginUSDT"`
	Leverage            int          `json:"leverage"`
	OpenType            OpenType     `json:"openType"`
	PositionMode        PositionMode `json:"positionMode"`
	MinFundingRate      float64      `json:"minFundingRate"`

	// Reuses domain types directly — single source of truth.
	FundingReversion domain.FundingReversionConfig `json:"fundingReversion"`
	FundingTrap      domain.FundingTrapConfig      `json:"fundingTrap"`

	ParsedOpenType     int `json:"-"`
	ParsedPositionMode int `json:"-"`
}

// Config is the root configuration containing both System and Funding configs.
type Config struct {
	System  *SystemConfig
	Symbols []SymbolConfig
}

// TradingDefaults is a temporary parsing struct to extract the opaque tradingDefaults
// block from system config and merge into per-symbol configs.
type TradingDefaults struct {
	MinFundingRate      float64 `json:"minFundingRate"`
	MaxPriceDiffPercent float64 `json:"maxPriceDiffPercent"`
	Leverage            int     `json:"leverage"`
	OpenType            string  `json:"openType"`
	PositionMode        string  `json:"positionMode"`

	// Reuses domain types directly.
	FundingReversion domain.FundingReversionConfig `json:"fundingReversion"`
	FundingTrap      domain.FundingTrapConfig      `json:"fundingTrap"`
}

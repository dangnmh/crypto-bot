package config

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

// TrailingConfig holds configuration for the Trailing Stop mechanism.
type TrailingConfig struct {
	Enabled       bool    `json:"enabled"`
	ActivationPct float64 `json:"activationPct"`
	CallbackPct   float64 `json:"callbackPct"`
}

// DynamicPricingConfig holds multipliers for auto-calculating Slippage, TP, and SL.
type DynamicPricingConfig struct {
	Enabled             bool    `json:"enabled"`
	SlippageMode        string  `json:"slippageMode"`
	ObBufferPct         float64 `json:"obBufferPct"`
	ObMaxSlippagePct    float64 `json:"obMaxSlippagePct"`
	ObStep              string  `json:"obStep"`
	SpreadMultiplier    float64 `json:"spreadMultiplier"`
	TpFundingMultiplier float64 `json:"tpFundingMultiplier"`
	TpAtrMultiplier     float64 `json:"tpAtrMultiplier"`
	SlAtrMultiplier     float64 `json:"slAtrMultiplier"`
	SlFundingMultiplier float64 `json:"slFundingMultiplier"`

	TrapDepthMultiplier float64 `json:"trapDepthMultiplier"`
	MinTrapDepth        float64 `json:"minTrapDepth"`
	MaxTrapDepth        float64 `json:"maxTrapDepth"`
	TrapTpMultiplier    float64 `json:"trapTpMultiplier"`
	MinTrapTP           float64 `json:"minTrapTP"`
	MaxTrapTP           float64 `json:"maxTrapTP"`
	TrapSlMultiplier    float64 `json:"trapSlMultiplier"`
	MinTrapSL           float64 `json:"minTrapSL"`
	MaxTrapSL           float64 `json:"maxTrapSL"`

	TrailingActivationMultiplier float64 `json:"trailingActivationMultiplier"`
	MinActivation                float64 `json:"minActivation"`
	MaxActivation                float64 `json:"maxActivation"`
	TrailingCallbackMultiplier   float64 `json:"trailingCallbackMultiplier"`
	MinCallback                  float64 `json:"minCallback"`
	MaxCallback                  float64 `json:"maxCallback"`
}

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
	TakeProfitPct       float64      `json:"takeProfitPct"`
	StopLossPct         float64      `json:"stopLossPct"`

	DynamicPricing DynamicPricingConfig `json:"dynamicPricing"`
	TrailingConfig TrailingConfig       `json:"trailingConfig"`

	EnableHedgeTrap    *bool          `json:"enableHedgeTrap"`
	TrapDepthPct       float64        `json:"trapDepthPct"`
	TrapTakeProfitPct  float64        `json:"trapTakeProfitPct"`
	TrapStopLossPct    float64        `json:"trapStopLossPct"`
	TrapTrailingConfig TrailingConfig `json:"trapTrailingConfig"`

	ParsedOpenType     int `json:"-"`
	ParsedPositionMode int `json:"-"`
}

// Config is the root configuration containing both System and Funding configs.
type Config struct {
	System  *SystemConfig
	Symbols []SymbolConfig
}

// TradingDefaults is a temporary parsing struct to extract the opaque tradingDefaults block from system config.
type TradingDefaults struct {
	MinFundingRate      float64 `json:"minFundingRate"`
	MaxPriceDiffPercent float64 `json:"maxPriceDiffPercent"`
	Leverage            int     `json:"leverage"`
	OpenType            string  `json:"openType"`
	PositionMode        string  `json:"positionMode"`
	TakeProfitPct       float64 `json:"takeProfitPct"`
	StopLossPct         float64 `json:"stopLossPct"`

	DynamicPricing DynamicPricingConfig `json:"dynamicPricing"`
	TrailingConfig TrailingConfig       `json:"trailingConfig"`

	EnableHedgeTrap    *bool          `json:"enableHedgeTrap"`
	TrapDepthPct       float64        `json:"trapDepthPct"`
	TrapTakeProfitPct  float64        `json:"trapTakeProfitPct"`
	TrapStopLossPct    float64        `json:"trapStopLossPct"`
	TrapTrailingConfig TrailingConfig `json:"trapTrailingConfig"`
}

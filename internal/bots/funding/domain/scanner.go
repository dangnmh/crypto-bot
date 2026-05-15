package domain

import (
	"math"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/types"
)

// SlippageMode constants for DynamicPricingConfig.SlippageMode.
const (
	SlippageModeOBImbalance     = "OB_IMBALANCE"
	SlippageModeSpreadMultipler = "SPREAD_MULTIPLIER"
)

// MarketData holds live market prices from the store/WS.
type MarketData struct {
	LastPrice float64
	BestBid   float64
	BestAsk   float64
	Volume24  float64
	Amount24  float64
}

// ContractSpec holds exchange contract specifications.
type ContractSpec struct {
	PriceUnit    float64
	VolUnit      int
	MinVol       int
	PriceScale   int
	VolScale     int
	ContractSize float64
	TakerFeeRate float64
	MakerFeeRate float64
}

// TradeConfig holds the trading parameters passed from the config layer.
// This is a domain value object — it mirrors what the config provides
// but belongs to the domain, not the config package.
type TradeConfig struct {
	Symbol              string
	SimulateSettle      string
	MaxPriceDiffPercent float64
	MarginUSDT          float64
	Leverage            int
	FundingReversion    FundingReversionConfig
	FundingTrap         FundingTrapConfig

	// Parsed exchange-specific values.
	ParsedOpenType     int
	ParsedPositionMode int
}

// FundingReversionConfig holds configuration specific to the reversion strategy.
type FundingReversionConfig struct {
	Enabled           bool                 `json:"enabled"`
	TakeProfitPct     float64              `json:"takeProfitPct"`
	StopLossPct       float64              `json:"stopLossPct"`
	MaxLatency        types.Duration       `json:"maxLatency"`
	BufferTime        types.Duration       `json:"bufferTime"`
	HoldDuration      types.Duration       `json:"holdDuration"`
	PostSettleTimeout types.Duration       `json:"postSettleTimeout"`
	DynamicPricing    DynamicPricingConfig `json:"dynamicPricing"`
	Trailing          TrailingConfig       `json:"trailing"`
}

// TrailingConfig holds configuration for the Trailing Stop mechanism.
// Static fields are fallbacks; dynamic multipliers override them when DynamicPricing is enabled.
type TrailingConfig struct {
	Enabled       bool    `json:"enabled"`
	ActivationPct float64 `json:"activationPct"`
	CallbackPct   float64 `json:"callbackPct"`

	// Dynamic multipliers (FR-scaled). Populated from config, applied in PrepareDynamicPricing.
	ActivationMultiplier float64 `json:"activationMultiplier"`
	MinActivation        float64 `json:"minActivation"`
	MaxActivation        float64 `json:"maxActivation"`
	CallbackMultiplier   float64 `json:"callbackMultiplier"`
	MinCallback          float64 `json:"minCallback"`
	MaxCallback          float64 `json:"maxCallback"`
}

// FundingTrapConfig holds all straddle trap configuration in one place.
type FundingTrapConfig struct {
	Enabled           bool           `json:"enabled"`
	DepthPct          float64        `json:"depthPct"`
	TakeProfitPct     float64        `json:"takeProfitPct"`
	StopLossPct       float64        `json:"stopLossPct"`
	TrapAfterSettle   types.Duration `json:"trapAfterSettle"`
	HoldDuration      types.Duration `json:"holdDuration"`
	PostSettleTimeout types.Duration `json:"postSettleTimeout"`

	// Dynamic multipliers (FR-scaled).
	DepthMultiplier float64 `json:"depthMultiplier"`
	MinDepth        float64 `json:"minDepth"`
	MaxDepth        float64 `json:"maxDepth"`
	TpMultiplier    float64 `json:"tpMultiplier"`
	MinTP           float64 `json:"minTP"`
	MaxTP           float64 `json:"maxTP"`
	SlMultiplier    float64 `json:"slMultiplier"`
	MinSL           float64 `json:"minSL"`
	MaxSL           float64 `json:"maxSL"`

	Trailing TrailingConfig `json:"trailing"`
}

// DynamicPricingConfig holds the master toggle and multipliers for slippage, TP, and SL.
// Trap and trailing multipliers live in their respective config sections.
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
}

// IsHedgeTrapEnabled returns true if hedge trap is enabled.
func (tc *TradeConfig) IsHedgeTrapEnabled() bool {
	return tc.FundingTrap.Enabled
}

// TradeIntent captures the directional decision from funding rate analysis.
// It answers: "what symbol, which direction, and why?".
type TradeIntent struct {
	Symbol       string
	FundingRate  float64
	Side         shared.Side
	CloseSide    shared.Side
	RefPriceType string // "bestBid" or "bestAsk"
}

// TradePlan holds values calculated after enrichment (contract specs + market data).
type TradePlan struct {
	Volume         float64       // contracts to trade
	CoinScore      float64       // composite ranking score
	ExpectedProfit float64       // estimated net profit %
	ImpactRatio    float64       // position size / 24h volume
	Slippage       float64       // estimated slippage %
	ATR            float64       // Average True Range (Dynamic Pricing)
	SafetyResult   *SafetyResult // safety evaluation result
}

// Candidate represents a coin that passed initial FR screening.
// Composed of embedded value objects for clean field access.
type Candidate struct {
	Config TradeConfig // Domain trade config

	TradeIntent  // Embedded — c.Symbol, c.Side etc. work directly
	ContractSpec // Enriched from contract details
	MarketData   // From ticker / WS push
	TradePlan    // Calculated values

	// Phase is a lightweight status tag for logging/flow control.
	// Primary FSM state lives in CycleState, not here.
	Phase Phase // "SCANNING", "ARMED", "FIRED_IOC", "FIRED_TRAP"
}

// ScanResult holds a scanned ticker for domain processing.
type ScanResult struct {
	Symbol      string
	FundingRate float64
	LastPrice   float64
	BestBid     float64
	BestAsk     float64
	Volume24    float64
	Amount24    float64
}

// ScanConfig holds per-symbol scan parameters (extracted from config layer).
type ScanConfig struct {
	Symbol         string
	MinFundingRate float64
}

// ScanFundingRates checks funding rates for configured symbols.
// Skips symbols with |FR| < minFundingRate.
// Determines side based on FR sign: FR>0 → LONG (receive), FR<0 → SHORT (receive).
func ScanFundingRates(tickers []ScanResult, configs []ScanConfig) []Candidate {
	tickerMap := make(map[string]*ScanResult, len(tickers))
	for i := range tickers {
		tickerMap[tickers[i].Symbol] = &tickers[i]
	}

	var candidates []Candidate

	for _, sc := range configs {
		t, ok := tickerMap[sc.Symbol]
		if !ok {
			continue
		}

		absFR := math.Abs(t.FundingRate)
		if absFR < sc.MinFundingRate {
			continue
		}

		c := Candidate{
			TradeIntent: TradeIntent{
				Symbol:      t.Symbol,
				FundingRate: t.FundingRate,
			},
			MarketData: MarketData{
				LastPrice: t.LastPrice,
				BestBid:   t.BestBid,
				BestAsk:   t.BestAsk,
				Volume24:  t.Volume24,
				Amount24:  t.Amount24,
			},
			Phase: PhaseScanning,
		}

		// ⭐ Side determination — Reversion strategy
		// FR positive → snipers went SHORT → they close (buy) → price pumps → we go LONG
		if t.FundingRate > 0 {
			c.Side = shared.SideOpenLong
			c.CloseSide = shared.SideCloseLong
			c.RefPriceType = "bestAsk"
		} else {
			// FR negative → snipers went LONG → they close (sell) → price dumps → we go SHORT
			c.Side = shared.SideOpenShort
			c.CloseSide = shared.SideCloseShort
			c.RefPriceType = "bestBid"
		}

		candidates = append(candidates, c)
	}

	return candidates
}

// EnrichWithContractSpec populates contract-specific fields from specs.
func EnrichWithContractSpec(candidates []Candidate, specs map[string]ContractSpec) {
	for i := range candidates {
		if spec, ok := specs[candidates[i].Symbol]; ok {
			candidates[i].ContractSpec = spec
		}
	}
}

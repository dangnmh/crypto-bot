package domain

import (
	"math"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/types"
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
	Enabled           bool           `json:"enabled"`
	TakeProfitPct     float64        `json:"takeProfitPct"`
	StopLossPct       float64        `json:"stopLossPct"`
	MaxLatency        types.Duration `json:"maxLatency"`
	BufferTime        types.Duration `json:"bufferTime"`
	PostSettleTimeout types.Duration `json:"postSettleTimeout"`
}

// TrailingConfig holds configuration for the Trailing Stop mechanism.
type TrailingConfig struct {
	Enabled       bool    `json:"enabled"`
	ActivationPct float64 `json:"activationPct"`
	CallbackPct   float64 `json:"callbackPct"`
}

// FundingTrapConfig holds all straddle trap configuration in one place.
type FundingTrapConfig struct {
	Enabled           bool           `json:"enabled"`
	SizeRatio         float64        `json:"sizeRatio"`
	MaxNotionalUSDT   float64        `json:"maxNotionalUSDT"`
	DepthPct          float64        `json:"depthPct"`
	TakeProfitPct     float64        `json:"takeProfitPct"`
	StopLossPct       float64        `json:"stopLossPct"`
	TrapAfterSettle   types.Duration `json:"trapAfterSettle"`
	PostSettleTimeout types.Duration `json:"postSettleTimeout"`

	Trailing TrailingConfig `json:"trailing"`
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
	ImbalanceRatio float64       // near-book bid volume / ask volume
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

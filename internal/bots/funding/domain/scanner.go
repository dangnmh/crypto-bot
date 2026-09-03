package domain

import (
	"math"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/types"
)

// MarketData holds live market prices from the store/WS.
// ... (lines omitted for brevity, let's just keep the code structure)
// Actually we can specify targetContent for the imports and for the struct.
// Let's do them in two separate contiguous blocks if needed, but since they are non-contiguous edits, we should use multi_replace_file_content! Or wait, can we do a single replace_file_content for each or multi_replace_file_content?
// Rule 5: To edit multiple, non-adjacent lines of code in the same file, make a single call to multi_replace_file_content.
// Yes! Let's use multi_replace_file_content for domain/scanner.go.

// MarketData holds live market prices from the store/WS.
type MarketData struct {
	LastPrice float64
	BestBid   float64
	BestAsk   float64
	Volume24  float64
	Vol24USDT float64
}

// ContractSpec holds exchange contract specifications.
type ContractSpec struct {
	PriceUnit    float64
	VolUnit      int
	MinVol       int
	MaxVol       int
	PriceScale   int
	VolScale     int
	ContractSize float64
	TakerFeeRate float64
	MakerFeeRate float64
	MaxLeverage  int
}

// TradeConfig holds the trading parameters passed from the config layer.
// This is a domain value object — it mirrors what the config provides
// but belongs to the domain, not the config package.
type TradeConfig struct {
	Symbol              string
	Exchange            string
	SimulateSettle      string
	MaxPriceDiffPercent float64
	MarginUSDT          float64
	Leverage            int
	FundingReversion    FundingReversionConfig
	MinFundingRate      float64
	MinVol24USD         float64

	// Parsed exchange-specific values.
	ParsedOpenType     int
	ParsedPositionMode int
}

// FundingReversionConfig holds configuration specific to the reversion strategy.
type FundingReversionConfig struct {
	Enabled                 bool           `json:"enabled"`
	TakeProfitPct           float64        `json:"takeProfitPct"`
	StopLossPct             float64        `json:"stopLossPct"`
	MaxLatency              types.Duration `json:"maxLatency"`
	BufferTime              types.Duration `json:"bufferTime"`
	PostSettleTimeout       types.Duration `json:"postSettleTimeout"`
	EnablePnLTrailing       bool           `json:"enablePnLTrailing"`
	PnLTrailingDropPct      float64        `json:"pnlTrailingDropPct"`
	PnLTrailingConfirmTicks int            `json:"pnlTrailingConfirmTicks"`
}

// TradeIntent captures the directional decision from funding rate analysis.
// It answers: "what symbol, which direction, and why?".
type TradeIntent struct {
	Symbol       string
	FundingRate  float64
	Side         shared.Side
	CloseSide    shared.Side
	RefPriceType string // "bestBid" or "bestAsk"
	ExternalID   string `json:"external_id,omitempty"`
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

	TradeIntent            // Embedded — c.Symbol, c.Side etc. work directly
	ContractSpec           // Enriched from contract details
	MarketData             // From ticker / WS push
	TradePlan              // Calculated values
	SettleTime   time.Time `json:"settle_time"`
}

// ScanResult holds a scanned ticker for domain processing.
type ScanResult struct {
	Symbol       string
	FundingRate  float64
	LastPrice    float64
	BestBid      float64
	BestAsk      float64
	Volume24     float64
	AmountUSDT24 float64
}

// ScanConfig holds per-symbol scan parameters (extracted from config layer).
type ScanConfig struct {
	Symbol         string
	MinFundingRate float64
	MinVol24USD    float64
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
		amtUSDT := t.AmountUSDT24
		if amtUSDT == 0 && t.Volume24 > 0 && t.LastPrice > 0 {
			amtUSDT = t.Volume24 * t.LastPrice
		}
		if sc.MinVol24USD > 0 && amtUSDT < sc.MinVol24USD {
			continue
		}

		c := Candidate{
			Symbol:      t.Symbol,
			FundingRate: t.FundingRate,
			LastPrice:   t.LastPrice,
			BestBid:     t.BestBid,
			BestAsk:     t.BestAsk,
			Volume24:    t.Volume24,
			Vol24USDT:   amtUSDT,
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

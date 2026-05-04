package domain

import (
	"math"
	"time"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/exchange"
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

// Candidate represents a coin that passed initial FR screening.
type Candidate struct {
	Config       config.SymbolConfig // Attached configuration
	Symbol       string
	FundingRate  float64
	Side         int    // 1=OpenLong (FR<0), 3=OpenShort (FR>0)
	CloseSide    int    // 4=CloseLong, 2=CloseShort
	RefPriceType string // "bestBid" or "bestAsk"

	ContractSpec // Enriched from contract details
	MarketData   // From ticker / WS push

	// Calculated
	Volume         float64 // contracts to trade
	CoinScore      float64
	ExpectedProfit float64
	ImpactRatio    float64
	Slippage       float64
	ATR            float64 // Average True Range (Dynamic Pricing)
	SafetyResult   *SafetyResult

	// State
	Phase      string    // "SCANNING", "ARMED", "FIRED", "CLOSING", "DONE"
	SettleTime time.Time // next funding settlement time for this symbol
}

// ScanFundingRates checks funding rates for configured symbols.
// Skips symbols with |FR| < minFundingRate (defined per symbol).
// Determines side based on FR sign: FR>0 → SHORT (receive), FR<0 → LONG (receive).
func ScanFundingRates(tickers []exchange.Ticker, symbolConfigs []config.SymbolConfig) []Candidate {
	// Build ticker lookup by symbol
	tickerMap := make(map[string]*exchange.Ticker, len(tickers))
	for i := range tickers {
		tickerMap[tickers[i].Symbol] = &tickers[i]
	}

	var candidates []Candidate

	for _, sc := range symbolConfigs {
		t, ok := tickerMap[sc.Symbol]
		if !ok {
			continue // symbol not found in ticker data
		}

		absFR := math.Abs(t.FundingRate)
		if absFR < sc.MinFundingRate {
			continue
		}

		c := Candidate{
			Config:      sc,
			Symbol:      t.Symbol,
			FundingRate: t.FundingRate,
			MarketData: MarketData{
				LastPrice: t.LastPrice,
				BestBid:   t.Bid1,
				BestAsk:   t.Ask1,
				Volume24:  t.Volume24,
				Amount24:  t.Amount24,
			},
			Phase: "SCANNING",
		}

		// ⭐ Side determination — Reversion strategy (opposite of sniper)
		// FR positive → snipers went SHORT → they close (buy) → price pumps → we go LONG
		if t.FundingRate > 0 {
			c.Side = exchange.SideOpenLong       // 1
			c.CloseSide = exchange.SideCloseLong // 4
			c.RefPriceType = "bestAsk"
		} else {
			// FR negative → snipers went LONG → they close (sell) → price dumps → we go SHORT
			c.Side = exchange.SideOpenShort       // 3
			c.CloseSide = exchange.SideCloseShort // 2
			c.RefPriceType = "bestBid"
		}

		candidates = append(candidates, c)
	}

	return candidates
}

// EnrichWithContractDetails populates contract-specific fields from contract details.
func EnrichWithContractDetails(candidates []Candidate, details []exchange.ContractDetail) {
	detailMap := make(map[string]*exchange.ContractDetail, len(details))
	for i := range details {
		detailMap[details[i].Symbol] = &details[i]
	}

	for i := range candidates {
		d, ok := detailMap[candidates[i].Symbol]
		if !ok {
			continue
		}
		candidates[i].ContractSpec = ContractSpec{
			PriceUnit:    d.PriceUnit,
			VolUnit:      d.VolUnit,
			MinVol:       d.MinVol,
			PriceScale:   d.PriceScale,
			VolScale:     d.VolScale,
			ContractSize: d.ContractSize,
			TakerFeeRate: d.TakerFeeRate,
			MakerFeeRate: d.MakerFeeRate,
		}
	}
}

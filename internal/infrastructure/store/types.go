package store

import (
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// PriceData holds the latest realtime price for a symbol (from WS push).
type PriceData struct {
	Symbol    string
	LastPrice float64
	BestBid   float64
	BestAsk   float64
	FairPrice float64
	Volume24  float64
	UpdatedAt time.Time
}

// TickerData holds ticker snapshot data (from REST sync).
type TickerData struct {
	Symbol       string
	Volume24     float64
	AmountUSDT24 float64
	LastPrice    float64
	BestBid      float64
	BestAsk      float64
	UpdatedAt    time.Time
}

// ContractData holds contract specification (from REST sync).
type ContractData struct {
	Symbol       string
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
	RiskLimits   []exchange.RiskLimitTier
	UpdatedAt    time.Time
}

// GetMaxVolForLeverage calculates maximum contract volume for a specific leverage and reference price,
// considering single-order maxQty (MaxVol) and leverage risk-limit tier caps.
func (c *ContractData) GetMaxVolForLeverage(leverage int, refPrice float64) int {
	maxVol := c.MaxVol
	if len(c.RiskLimits) == 0 || leverage <= 0 || refPrice <= 0 || c.ContractSize <= 0 {
		return maxVol
	}

	var bestTier *exchange.RiskLimitTier
	for i := range c.RiskLimits {
		tier := &c.RiskLimits[i]
		if tier.MaxLeverage >= leverage {
			if bestTier == nil || tier.MaxLeverage < bestTier.MaxLeverage {
				bestTier = tier
			}
		}
	}

	if bestTier != nil {
		if bestTier.MaxQuantity > 0 && int(bestTier.MaxQuantity) < maxVol {
			maxVol = int(bestTier.MaxQuantity)
		}
		if bestTier.MaxNotional > 0 {
			notionalMaxVol := int(bestTier.MaxNotional / (c.ContractSize * refPrice))
			if notionalMaxVol > 0 && notionalMaxVol < maxVol {
				maxVol = notionalMaxVol
			}
		}
	}

	return maxVol
}

// FundingData holds per-symbol funding rate detail (from REST sync).
type FundingData struct {
	Symbol         string
	FundingRate    float64
	NextSettleTime int64 // Unix ms
	MaxFundingRate float64
	MinFundingRate float64
	CollectCycle   int
	UpdatedAt      time.Time
}

// TickerDataFromExchange converts an exchange.Ticker to TickerData.
func TickerDataFromExchange(t *exchange.Ticker) *TickerData {
	amt := t.AmountUSDT24
	if amt == 0 && t.Volume24 > 0 && t.LastPrice > 0 {
		amt = t.Volume24 * t.LastPrice
	}
	return &TickerData{
		Symbol:       t.Symbol,
		Volume24:     t.Volume24,
		AmountUSDT24: amt,
		LastPrice:    t.LastPrice,
		BestBid:      t.Bid1,
		BestAsk:      t.Ask1,
		UpdatedAt:    time.Now(),
	}
}

// ContractDataFromExchange converts an exchange.ContractDetail to ContractData.
func ContractDataFromExchange(d *exchange.ContractDetail) *ContractData {
	return &ContractData{
		Symbol:       d.Symbol,
		PriceUnit:    d.PriceUnit,
		VolUnit:      d.VolUnit,
		MinVol:       d.MinVol,
		MaxVol:       d.MaxVol,
		PriceScale:   d.PriceScale,
		VolScale:     d.VolScale,
		ContractSize: d.ContractSize,
		TakerFeeRate: d.TakerFeeRate,
		MakerFeeRate: d.MakerFeeRate,
		MaxLeverage:  d.MaxLeverage,
		RiskLimits:   d.RiskLimits,
		UpdatedAt:    time.Now(),
	}
}

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
	Symbol         string
	FundingRate    float64
	NextSettleTime int64 // Unix ms
	Volume24       float64
	Amount24       float64
	LastPrice      float64
	BestBid        float64
	BestAsk        float64
	UpdatedAt      time.Time
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
	UpdatedAt    time.Time
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
	return &TickerData{
		Symbol:         t.Symbol,
		FundingRate:    t.FundingRate,
		NextSettleTime: t.NextSettleTime,
		Volume24:       t.Volume24,
		Amount24:       t.Amount24,
		LastPrice:      t.LastPrice,
		BestBid:        t.Bid1,
		BestAsk:        t.Ask1,
		UpdatedAt:      time.Now(),
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
		UpdatedAt:    time.Now(),
	}
}

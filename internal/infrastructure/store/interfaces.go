package store

import (
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// ──────────────────────────────────────────────────────────────────────
// Reader interfaces — used by bots to consume store data (read-only)
// ──────────────────────────────────────────────────────────────────────.

// TickerReader provides access to REST-synced ticker data.
type TickerReader interface {
	GetTicker(symbol string) (*TickerData, error)
	GetAllTickers() []*TickerData
}

// ContractReader provides access to REST-synced contract specifications.
type ContractReader interface {
	GetContract(symbol string) (*ContractData, error)
}

// PriceReader provides access to real-time WS price data.
type PriceReader interface {
	GetPrice(symbol string, maxAge time.Duration) (*PriceData, error)
	GetBestBidAsk(symbol string) (bid, ask float64, err error)
	PriceAge(symbol string) time.Duration
}

// PriceWriter allows updating real-time price data (used by EventRouter).
type PriceWriter interface {
	UpdatePrice(symbol string, data *PriceData)
}

// DepthReader provides access to L2/L3 order book data.
type DepthReader interface {
	GetDepth(symbol string) (*exchange.OrderBook, error)
}

// DepthWriter allows updating order book data (used by OrderBookManager).
type DepthWriter interface {
	UpdateDepth(symbol string, ob *exchange.OrderBook)
}

// FundingReader provides access to funding rate and settlement data.
type FundingReader interface {
	GetFunding(symbol string) (*FundingData, error)
	GetSettleTime(symbol string) (time.Time, error)
}

// KlineReader provides access to candlestick data.
type KlineReader interface {
	GetKlines(symbol string) []exchange.Kline
}

// KlineWriter allows inserting kline data.
type KlineWriter interface {
	AddKline(symbol string, k exchange.Kline)
	InitKlines(symbol string, maxLen int, initial []exchange.Kline)
}

// KlineReadWriter combines read and write access to kline data.
type KlineReadWriter interface {
	KlineReader
	KlineWriter
}

// ──────────────────────────────────────────────────────────────────────
// Compile-time interface compliance checks
// ──────────────────────────────────────────────────────────────────────.

var (
	_ TickerReader   = (*TickerStore)(nil)
	_ ContractReader = (*ContractStore)(nil)
	_ PriceReader    = (*PriceStore)(nil)
	_ PriceWriter    = (*PriceStore)(nil)
	_ DepthReader    = (*DepthStore)(nil)
	_ DepthWriter    = (*DepthStore)(nil)
	_ FundingReader  = (*FundingStore)(nil)
	_ KlineReader    = (*KlineStore)(nil)
	_ KlineWriter    = (*KlineStore)(nil)
)

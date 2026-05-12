package store

import (
	"context"
	"time"

	"crypto-bot/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────
// Reader interfaces — used by bots to consume store data (read-only)
// ──────────────────────────────────────────────────────────────────────.

// TickerReader provides access to REST-synced ticker data.
type TickerReader interface {
	GetTicker(ctx context.Context, symbol string) (*TickerData, error)
	GetAllTickers(ctx context.Context) []*TickerData
}

// ContractReader provides access to REST-synced contract specifications.
type ContractReader interface {
	GetContract(ctx context.Context, symbol string) (*ContractData, error)
}

// PriceReader provides access to real-time WS price data.
type PriceReader interface {
	GetPrice(ctx context.Context, symbol string, maxAge time.Duration) (*PriceData, error)
	GetBestBidAsk(ctx context.Context, symbol string) (bid, ask float64, err error)
	PriceAge(symbol string) time.Duration
}

// PriceWriter allows updating real-time price data (used by EventRouter).
type PriceWriter interface {
	UpdatePrice(symbol string, data *PriceData)
}

// DepthReader provides access to L2/L3 order book data.
type DepthReader interface {
	GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error)
}

// DepthWriter allows updating order book data (used by OrderBookManager).
type DepthWriter interface {
	UpdateDepth(symbol string, ob *domain.OrderBook)
}

// FundingReader provides access to funding rate and settlement data.
type FundingReader interface {
	GetFunding(ctx context.Context, symbol string) (*FundingData, error)
	GetSettleTime(ctx context.Context, symbol string) (time.Time, error)
}

// KlineReader provides access to candlestick data.
type KlineReader interface {
	GetKlines(ctx context.Context, symbol string) []domain.Kline
}

// KlineWriter allows inserting kline data.
type KlineWriter interface {
	AddKline(symbol string, k domain.Kline)
	InitKlines(symbol string, maxLen int, initial []domain.Kline)
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

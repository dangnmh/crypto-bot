package dilution

import (
	"context"
	"time"

	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/trading/ordermanager/futures"
)

// EngineProviderGetter decouples exchange client resolution from the full engine.
type EngineProviderGetter interface {
	GetProvider(name string) (*infraapp.ExchangeProvider, error)
}

// OrderManagerDispatcher sends order events and cancellation requests to the trading OrderManager.
type OrderManagerDispatcher interface {
	Dispatch(ctx context.Context, event futures.OrderEvent) error
	CancelOpenOrders(ctx context.Context, exchangeName, symbol string) error
}

// DilutionSpec defines the specifications for an ambient PostOnly maker quote order.
type DilutionSpec struct {
	ReqID                 string
	Exchange              string
	Symbol                string
	Side                  shared.Side
	NotionalUSDT          float64
	MarginUSDT            float64
	Leverage              int
	Price                 float64
	Volume                float64
	ContractSize          float64
	PositionCloseTimeout  time.Duration
	UnfilledCancelTimeout time.Duration
	TakeProfitPrice       float64
	StopLossPrice         float64
	OrderType             futures.OrderType
	Vol24hUSDT            float64
}

// MarketInfo holds parsed exchange market metadata for quoting.
type MarketInfo struct {
	BestBid      float64
	BestAsk      float64
	LastPrice    float64
	PriceUnit    float64
	PriceScale   int
	MinVol       int
	VolScale     int
	ContractSize float64
	Vol24hUSDT   float64
}

// PositionSummary provides granular long/short exposure breakdown for inventory management.
type PositionSummary struct {
	LongVol  float64
	LongUSD  float64
	ShortVol float64
	ShortUSD float64
	NetUSD   float64
	GrossUSD float64
}

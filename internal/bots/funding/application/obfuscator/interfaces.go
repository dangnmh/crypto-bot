package obfuscator

import (
	"context"
	"fmt"
	"time"

	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/trading/ordermanager"
	ordermanagerpersistence "crypto-bot/internal/trading/ordermanager/persistence"
)

// EngineProviderGetter retrieves an ExchangeProvider by exchange name at runtime.
type EngineProviderGetter interface {
	GetProvider(name string) (*infraapp.ExchangeProvider, error)
}

// PnLReportReader queries aggregated symbol PnL metrics for loss budget obfuscation logic.
type PnLReportReader interface {
	GetSymbolPnLSummaries(ctx context.Context, exchange string, since time.Time) ([]ordermanagerpersistence.SymbolPnLSummary, error)
}

// OrderManagerDispatcher defines the order execution contract with OrderManager.
type OrderManagerDispatcher interface {
	Dispatch(ctx context.Context, evt ordermanager.OrderEvent) error
}

// MarketInfo contains pricing, depth micro-momentum, and contract specifications.
type MarketInfo struct {
	Side         shared.Side
	RefPrice     float64
	LastPrice    float64
	BestBid      float64
	BestAsk      float64
	ContractSize float64
	MinVol       int
	VolScale     int
	PriceUnit    float64
	PriceScale   int
	Vol24hUSDT   float64
	FundingRate  float64
}

// ObfuscationSpec contains parameters for a generated dummy order.
type ObfuscationSpec struct {
	OriginReqID     string
	Exchange        string
	Symbol          string
	Side            shared.Side
	NotionalUSDT    float64
	MarginUSDT      float64
	Leverage        int
	Price           float64
	Volume          float64
	ContractSize    float64
	TakeProfitPrice float64
	StopLossPrice   float64
	TakeProfitPct   float64
	StopLossPct     float64
	HoldDuration    time.Duration
	OrderType       ordermanager.OrderType
	Vol24hUSDT      float64
	FundingRate     float64
}

var _ OrderManagerDispatcher = (*EventBusDispatcher)(nil)

// EventPublisher defines basic event bus publishing contract.
type EventPublisher interface {
	Publish(topic string, payload any) error
}

// EventBusDispatcher wraps an EventPublisher to implement OrderManagerDispatcher.
type EventBusDispatcher struct {
	publisher EventPublisher
}

// NewEventBusDispatcher creates a new EventBusDispatcher adapter.
func NewEventBusDispatcher(pub EventPublisher) (*EventBusDispatcher, error) {
	if pub == nil {
		return nil, fmt.Errorf("missing required dependency EventPublisher for EventBusDispatcher")
	}
	return &EventBusDispatcher{publisher: pub}, nil
}

// Dispatch publishes the order event to the event publisher.
func (d *EventBusDispatcher) Dispatch(ctx context.Context, evt ordermanager.OrderEvent) error {
	if evt == nil {
		return nil
	}
	return d.publisher.Publish(evt.GetTopic(), evt)
}

package common

import (
	"context"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
)

// Notifier is an alias for notifier.Notifier interface.
type Notifier = notifier.Notifier

// NotiLevelProvider allows execution events to specify custom notification severity levels.
type NotiLevelProvider interface {
	GetNotiLevel() notifier.Level
}

// Clock provides time and latency queries.
type Clock interface {
	Now() time.Time
	LatencyMs() int64
	Until(t time.Time) time.Duration
	Sleep(ctx context.Context, d time.Duration) error
}

// SyncerClock represents a clock capable of forcing time synchronization.
type SyncerClock interface {
	SyncNow(ctx context.Context)
}

// MarketType identifies whether the target market is Spot or Future/Perpetual.
type MarketType string

const (
	MarketTypeSpot    MarketType = "SPOT"
	MarketTypeFutures MarketType = "FUTURE"
)

// StrategyType identifies which trading strategy generated the order.
type StrategyType string

const (
	StrategyFundingReversion StrategyType = "FUNDING_REVERSION"
	StrategyFundingArbitrage StrategyType = "FUNDING_ARBITRAGE"
	StrategyPennyJumper      StrategyType = "PENNY_JUMPER"
	StrategyObfuscator       StrategyType = "OBFUSCATOR"
	StrategyDilution         StrategyType = "DILUTION"
	StrategyGrid             StrategyType = "GRID"
	StrategyUnknown          StrategyType = "UNKNOWN"
)

// OrderType identifies the execution order type.
type OrderType string

const (
	OrderTypeMarket   OrderType = "MARKET"
	OrderTypeLimit    OrderType = "LIMIT"
	OrderTypePostOnly OrderType = "POST_ONLY"
	OrderTypeIOC      OrderType = "IOC"
	OrderTypeFOK      OrderType = "FOK"
)

// ToDomain converts OrderType to domain.OrderType.
func (t OrderType) ToDomain() shared.OrderType {
	switch t {
	case OrderTypeMarket:
		return shared.OrderTypeMarket
	case OrderTypeLimit:
		return shared.OrderTypeLimit
	case OrderTypePostOnly:
		return shared.OrderTypePostOnly
	case OrderTypeIOC:
		return shared.OrderTypeIOC
	case OrderTypeFOK:
		return shared.OrderTypeFOK
	default:
		return shared.OrderTypeLimit
	}
}

// IsMaker returns true if the order type is a passive maker order (POST_ONLY or LIMIT).
func (t OrderType) IsMaker() bool {
	return t == OrderTypePostOnly || t == OrderTypeLimit
}

// IsTaker returns true if the order type is an aggressive taker order (IOC or MARKET or FOK).
func (t OrderType) IsTaker() bool {
	return t == OrderTypeIOC || t == OrderTypeMarket || t == OrderTypeFOK
}

// OrderOutcome represents the terminal execution outcome of an order.
type OrderOutcome string

const (
	OutcomeFilled          OrderOutcome = "filled"
	OutcomePartialFilled   OrderOutcome = "partial_filled"
	OutcomePartiallyFilled OrderOutcome = "partial_filled"
	OutcomeCanceledNoFill  OrderOutcome = "canceled_no_fill"
	OutcomeCanceled        OrderOutcome = "canceled"
	OutcomeAborted         OrderOutcome = "aborted"
	OutcomeBailout         OrderOutcome = "bailout"
	OutcomeResting         OrderOutcome = "resting"
	OutcomeRejected        OrderOutcome = "rejected"
	OutcomeExpired         OrderOutcome = "expired"
	OutcomeUnknown         OrderOutcome = "unknown"
)

// TradeRepository interface for persisting trade records.
type TradeRepository interface {
	Save(ctx context.Context, event OrderTradeRecordEvent) error
}

// OrderEvent is the base contract for all order execution events.
type OrderEvent interface {
	GetReqID() string
	GetClientOrderID() string
	GetSymbol() string
	GetExchange() string
	GetMarketType() MarketType
	GetStrategyType() StrategyType
	GetPreTopic() string
	GetNextTopic() string
	GetTimestamp() time.Time
	GetTopic() string
	ShouldNotify() bool
	GetNotifyMessage() string
	DeduplicateKey() string
}

// BaseOrderEvent provides standard implementations for common event fields.
type BaseOrderEvent struct {
	ReqID         string       `json:"req_id"`
	ClientOrderID string       `json:"client_order_id"`
	Symbol        string       `json:"symbol"`
	Exchange      string       `json:"exchange"`
	MarketType    MarketType   `json:"market_type"`
	StrategyType  StrategyType `json:"strategy_type"`
	PreTopic      string       `json:"pre_topic"`
	NextTopic     string       `json:"next_topic"`
	Timestamp     time.Time    `json:"timestamp"`
}

func (e BaseOrderEvent) GetReqID() string              { return e.ReqID }
func (e BaseOrderEvent) GetClientOrderID() string      { return e.ClientOrderID }
func (e BaseOrderEvent) GetSymbol() string             { return e.Symbol }
func (e BaseOrderEvent) GetExchange() string           { return e.Exchange }
func (e BaseOrderEvent) GetMarketType() MarketType     { return e.MarketType }
func (e BaseOrderEvent) GetStrategyType() StrategyType { return e.StrategyType }
func (e BaseOrderEvent) GetPreTopic() string           { return e.PreTopic }
func (e BaseOrderEvent) GetNextTopic() string          { return e.NextTopic }
func (e BaseOrderEvent) GetTimestamp() time.Time       { return e.Timestamp }
func (e BaseOrderEvent) ShouldNotify() bool            { return false }
func (e BaseOrderEvent) GetNotifyMessage() string      { return "" }
func (e BaseOrderEvent) DeduplicateKey() string {
	if e.NextTopic != "" {
		return e.ReqID + "-" + e.NextTopic
	}
	return e.ReqID + "-" + string(e.StrategyType)
}

// ClosedPnLProvider enriches actual trade fill prices and net profit metrics.
type ClosedPnLProvider interface {
	GetOrderPNL(ctx context.Context, symbol string, orderID string) (*exchange.ClosedPnLInfo, error)
}

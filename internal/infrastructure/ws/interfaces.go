package ws

import (
	"context"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
)

// Subscriber handles channel subscription/unsubscription.
// Satisfied by *Client. Enables mock-based testing of executor.
type Subscriber interface {
	SubscribeTicker(ctx context.Context, symbol string) error
	UnsubscribeTicker(ctx context.Context, symbol string) error

	SubscribePersonal(ctx context.Context) error
	UnsubscribePersonal(ctx context.Context) error
	SubscribePublic(ctx context.Context, topic string, subMsg any) error
	UnsubscribePublic(ctx context.Context, topic string, unsubMsg any) error
}

// DepthSubscriber handles depth streaming subscription/unsubscription.
type DepthSubscriber interface {
	SubscribeDepth(ctx context.Context, symbol string) error
	UnsubscribeDepth(ctx context.Context, symbol string) error
}

// TradeSubscriber handles trade streaming subscription/unsubscription.
type TradeSubscriber interface {
	SubscribeTrade(ctx context.Context, symbol string) error
	UnsubscribeTrade(ctx context.Context, symbol string) error
}

// ExchangeAdapter encapsulates all exchange-specific WS logic.
type ExchangeAdapter interface {
	Subscriber // Inherit Subscribe, Unsubscribe methods
	ExchangeAdapterParser
}

type ExchangeAdapterParser interface {
	// SetPool allows the Engine to inject the WS pool after initialization.
	SetPool(pool *pkgws.Pool)

	// Auth & Routing.
	GetPingConfig() (payload any, interval time.Duration)
	GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client)
	GetChannelExtractor() func([]byte) string

	// Parsers (raw JSON []byte to domain objects).
	ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error)
	ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error)
	ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error)
}

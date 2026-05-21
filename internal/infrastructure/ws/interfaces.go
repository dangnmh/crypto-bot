package ws

import (
	"context"
	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"time"

	pkgws "crypto-bot/pkg/ws"
)

// Subscriber handles channel subscription/unsubscription.
// Satisfied by *Client. Enables mock-based testing of executor.
type Subscriber interface {
	SubscribeTicker(ctx context.Context, symbol string) error
	UnsubscribeTicker(ctx context.Context, symbol string) error

	SubscribeKline(ctx context.Context, symbol string) error
	UnsubscribeKline(ctx context.Context, symbol string) error

	SubscribeDepth(ctx context.Context, symbol, step string) error
	UnsubscribeDepth(ctx context.Context, symbol, step string) error

	SubscribePersonal(ctx context.Context) error
}

// ExchangeAdapter encapsulates all exchange-specific WS logic.
type ExchangeAdapter interface {
	Subscriber // Inherit Subscribe, Unsubscribe methods

	// SetPool allows the Engine to inject the WS pool after initialization.
	SetPool(pool *pkgws.Pool)

	// Auth & Routing.
	GetPingConfig() (payload interface{}, interval time.Duration)
	GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client)
	GetChannelExtractor() func([]byte) string

	// Parsers (raw JSON []byte to domain objects).
	ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error)
	ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error)
	ParseKline(data []byte) (symbol string, k *domain.Kline, err error)
	ParseOrder(data []byte) (*exchange.WsOrderDeal, error)
	ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error)
	ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error)
	ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error)
}

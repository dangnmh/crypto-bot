package ws

import (
	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"time"

	pkgws "crypto-bot/pkg/ws"
)

// Subscriber handles channel subscription/unsubscription.
// Satisfied by *Client. Enables mock-based testing of executor.
type Subscriber interface {
	SubscribeTicker(symbol string) error
	UnsubscribeTicker(symbol string) error

	SubscribeKline(symbol string) error
	UnsubscribeKline(symbol string) error

	SubscribeDepth(symbol, step string) error
	UnsubscribeDepth(symbol, step string) error

	SubscribePersonal() error
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
}

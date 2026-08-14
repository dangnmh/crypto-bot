package strategy

import (
	"context"
	"log/slog"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	infrawatcher "crypto-bot/internal/infrastructure/watcher"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	pkgws "crypto-bot/pkg/ws"
)

// Deps holds all external dependencies for a funding cycle.
type Deps struct {
	Client        exchange.Client
	OrderNotifier infrawatcher.OrderNotifier
	TickerStore   store.TickerReader
	ContractStore store.ContractReader
	PriceStore    store.PriceReader
	FundingStore  store.FundingReader
	DepthStore    store.DepthReader
	Clock         shared.Clock
	Log           *slog.Logger
	Notifier      notifier.Notifier
	EventBus      *eventbus.Bus
	WsSub         infraws.ExchangeManagerAdapterSubscriber
}

// FundingStoreSet defines the exchange-specific store requirements.
type FundingStoreSet interface {
	Start(ctx context.Context)
	WaitReady(ctx context.Context) error
	WireWS(pool *pkgws.Pool, adapter infraws.ExchangeAdapterParser)
	Ticker() store.TickerReader
	Contract() store.ContractReader
	Price() store.PriceReader
	Funding() store.FundingReader
	Depth() store.DepthReader
	Kline() store.KlineReadWriter
}

// BackgroundStrategy represents a global trading workflow that is initialized ONCE.
type BackgroundStrategy interface {
	// Start is called exactly once during bot startup.
	// It receives the map of exchange-specific stores and initializes the EventBus consumers.
	Start(ctx context.Context, stores map[string]FundingStoreSet) error

	// Stop cleans up global resources on shutdown.
	Stop(ctx context.Context) error
}

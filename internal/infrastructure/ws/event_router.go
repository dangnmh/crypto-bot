package ws

import (
	"log/slog"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/cskr/pubsub"
)

// EventRouter wires standard WS push channels to their respective stores.
// It eliminates the copy-paste wireWSEvents() pattern across bots.
type EventRouter struct {
	pool          *pkgws.Pool
	adapter       ExchangeAdapter
	priceStore    *store.PriceStore
	depthStore    *store.DepthStore
	bus           *pubsub.PubSub
	klineStore    *store.KlineStore // optional
	logCfg        sysconfig.LoggingConfig
}

// EventRouterConfig groups the dependencies for EventRouter.
type EventRouterConfig struct {
	Pool          *pkgws.Pool
	Adapter       ExchangeAdapter
	PriceStore    *store.PriceStore
	DepthStore    *store.DepthStore
	Bus           *pubsub.PubSub
	KlineStore    *store.KlineStore // nil if bot doesn't need klines
	LogCfg        sysconfig.LoggingConfig
}

// NewEventRouter creates a new EventRouter.
func NewEventRouter(cfg EventRouterConfig) *EventRouter {
	return &EventRouter{
		pool:          cfg.Pool,
		adapter:       cfg.Adapter,
		priceStore:    cfg.PriceStore,
		depthStore:    cfg.DepthStore,
		bus:           cfg.Bus,
		klineStore:    cfg.KlineStore,
		logCfg:        cfg.LogCfg,
	}
}

// Setup registers all standard WS event handlers.
func (r *EventRouter) Setup() {
	r.setupTicker()
	r.setupDepth()
	r.setupPersonal()

	if r.klineStore != nil {
		r.setupKline()
	}
}

func (r *EventRouter) setupTicker() {
	r.pool.On("ticker", func(data []byte) {
		symbol, pd, err := r.adapter.ParseTicker(data)
		if err == nil && symbol != "" && pd != nil {
			r.priceStore.UpdatePrice(symbol, pd)
		}
	})
}

func (r *EventRouter) setupDepth() {
	r.pool.On("depth", func(data []byte) {
		symbol, ob, err := r.adapter.ParseDepth(data)
		if err == nil && symbol != "" && ob != nil {
			r.depthStore.UpdateDepth(symbol, ob)
		}
	})
}

func (r *EventRouter) setupKline() {
	r.pool.On("kline", func(data []byte) {
		symbol, k, err := r.adapter.ParseKline(data)
		if err == nil && symbol != "" && k != nil {
			r.klineStore.AddKline(symbol, *k)
		}
	})
}

func (r *EventRouter) setupPersonal() {
	r.pool.On("personal.order", func(data []byte) {
		if r.logCfg.WS.Order {
			slog.Info("Raw personal order payload", "data", string(data))
		}
		deal, err := r.adapter.ParseOrder(data)
		if err == nil && deal != nil {
			r.bus.Pub(*deal, deal.GetOrderID())
		}
	})

	r.pool.On("personal.position", func(data []byte) {
		if r.logCfg.WS.Position {
			slog.Info("Raw personal position payload", "data", string(data))
		}
	})
}

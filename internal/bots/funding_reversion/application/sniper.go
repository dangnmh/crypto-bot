package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"

	"golang.org/x/sync/errgroup"
)

// CloseResult holds the outcome of closing a single position.
type CloseResult struct {
	Symbol      string
	ExitOrderID string
	ExitPrice   float64
	ExitVol     float64
	Closed      bool
	TakerFee    float64
	MakerFee    float64
	Profit      float64
}

// ──────────────────────────────────────────────────────────────────────
// Sniper — top-level orchestrator
// ──────────────────────────────────────────────────────────────────────.

// Sniper spawns one independent worker goroutine per configured symbol.
type Sniper struct {
	cfg           *config.Config
	sysCfg        *config.SystemConfig
	engine        *app.Engine
	client        exchange.Client
	ws            ws.Subscriber
	orderNotifier *watcher.OrderWatcher
	stores        *app.StoreRegistry
	timeSync      *timesync.TimeSync
}

// NewSniper creates a new Sniper instance.
func NewSniper(cfg *config.Config, sysCfg *config.SystemConfig, engine *app.Engine) *Sniper {
	wsSub := engine.Adapter
	orderWatcher := watcher.NewOrderWatcher(engine.Bus, slog.Default().With("component", "funding_reversion_order_watcher"))

	stores := app.NewStoreRegistry().WithFunding().WithKline()

	return &Sniper{
		cfg:           cfg,
		sysCfg:        sysCfg,
		engine:        engine,
		client:        engine.Client,
		ws:            wsSub,
		orderNotifier: orderWatcher,
		stores:        stores,
		timeSync:      engine.TimeSync,
	}
}

// RunAsBackground launches all required sync and connection routines for Funding Reversion.
func (s *Sniper) RunAsBackground(ctx context.Context) error {
	// 1. WarmUp + TimeSync
	go s.engine.Client.WarmUp(ctx, 4*time.Second)
	go s.engine.TimeSync.Start(ctx)
	s.engine.TimeSync.WaitReady(ctx)

	// 2. Build funding symbols list
	var fundingSymbols []string
	for _, sym := range s.cfg.Symbols {
		fundingSymbols = append(fundingSymbols, sym.Symbol)
	}

	// 3. Start stores + wait for initial data
	s.stores.StartStores(ctx, s.engine, app.StoreSyncConfig{
		TickerInterval:   s.sysCfg.Sync.Ticker,
		ContractInterval: s.sysCfg.Sync.Contract,
		FundingInterval:  s.sysCfg.Sync.FundingSync,
		FundingSymbols:   fundingSymbols,
	})
	if err := s.stores.WaitReady(ctx); err != nil {
		return err
	}

	// 4. Connect WS + subscribe personal channels
	go s.engine.WS.Connect(ctx)
	s.engine.WS.WaitReady(ctx)

	if s.engine.Adapter != nil {
		_ = s.engine.Adapter.SubscribePersonal()
	}

	slog.Info("🟢 Funding Reversion Background Services Ready")
	return nil
}

// Run starts all symbol workers. Blocks until all stop or context is cancelled.
func (s *Sniper) Run(ctx context.Context) error {
	slog.Info("🚀 Sniper — launching per-symbol workers", "symbols", len(s.cfg.Symbols))

	g, workerCtx := errgroup.WithContext(ctx)
	for _, sc := range s.cfg.Symbols {
		g.Go(s.spawnWorker(ctx, workerCtx, sc))
	}

	err := g.Wait()
	slog.Info("🛑 All workers stopped")
	return err
}

// Stop implements the app.Bot interface. It executes any explicit teardown.
// The primary graceful shutdown is handled by the context passed to Run().
func (s *Sniper) Stop(ctx context.Context) error {
	slog.Info("🛑 Sniper explicit stop invoked")
	return nil
}

func (s *Sniper) spawnWorker(parentCtx, workerCtx context.Context, symCfg config.SymbolConfig) func() error {
	return func() error {
		log := slog.Default().With("w", "sniper", "sym", symCfg.Symbol)
		w := &symbolWorker{
			cfg:           symCfg,
			global:        s.cfg,
			client:        s.client,
			ws:            s.ws,
			orderNotifier: s.orderNotifier,
			tickerStore:   s.stores.Ticker,
			contractStore: s.stores.Contract,
			priceStore:    s.stores.Price,
			fundingStore:  s.stores.Funding,
			klineStore:    s.stores.Kline,
			depthStore:    s.stores.Depth,
			ts:            s.timeSync,
			log:           log,
			trailing:      NewTrailingManager(parentCtx, s.client, s.orderNotifier, log),
			subs:          NewSubscriptionManager(s.ws, symCfg.Symbol, toTradeConfig(symCfg).DynamicPricing, log),
		}
		w.log.Info("🚀 Worker started")
		w.run(workerCtx)
		w.log.Info("🛑 Worker stopped")
		return nil
	}
}

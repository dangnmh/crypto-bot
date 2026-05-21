package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
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
// Sniper — top-level orchestrator.
// ──────────────────────────────────────────────────────────────────────.

// Sniper spawns one independent worker goroutine per configured symbol.
type Sniper struct {
	cfg           *config.Config
	sysCfg        *config.SystemConfig
	engine        *app.Engine
	client        exchange.Client
	ws            ws.Subscriber
	orderNotifier *watcher.OrderWatcher
	stores        *app.CentralStore
	timeSync      shared.Clock
	disabled      map[string]string
	disabledMu    sync.RWMutex
	log           *slog.Logger
	bgWg          sync.WaitGroup // tracks background goroutines (§5.2)
}

// NewSniper creates a new Sniper instance.
func NewSniper(cfg *config.Config, sysCfg *config.SystemConfig, engine *app.Engine, log *slog.Logger) *Sniper {
	wsSub := engine.Adapter
	orderWatcher := watcher.NewOrderWatcher(engine.Bus, log.With("component", "order_watcher"))

	// Build funding symbols list for the FundingStore option.
	fundingSymbols := make([]string, 0, len(cfg.Symbols))
	for i := range cfg.Symbols {
		fundingSymbols = append(fundingSymbols, cfg.Symbols[i].Symbol)
	}

	stores := app.NewCentralStore(
		app.WithTicker(engine.Client, time.Duration(sysCfg.Sync.Ticker)),
		app.WithContract(engine.Client, time.Duration(sysCfg.Sync.Contract)),
		app.WithFunding(engine.Client, time.Duration(sysCfg.Sync.FundingSync), fundingSymbols),
		app.WithPrice(),
		app.WithDepth(),
		app.WithKline(),
	)

	return &Sniper{
		cfg:           cfg,
		sysCfg:        sysCfg,
		engine:        engine,
		client:        engine.Client,
		ws:            wsSub,
		orderNotifier: orderWatcher,
		stores:        stores,
		timeSync:      engine.TimeSync,
		disabled:      make(map[string]string),
		log:           log,
	}
}

// RunAsBackground launches all required sync and connection routines for Funding Reversion.
func (s *Sniper) RunAsBackground(ctx context.Context) error {
	// 1. WarmUp + TimeSync.
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		s.engine.Client.WarmUp(ctx, 4*time.Second)
	}()
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		s.engine.TimeSync.Start(ctx)
	}()
	s.engine.TimeSync.WaitReady(ctx)

	// 2. Start stores + wait for initial data.
	s.stores.Start(ctx)
	if err := s.stores.WaitReady(ctx); err != nil {
		return err
	}

	// 3. Connect WS + subscribe personal channels.
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		s.engine.WS.Connect(ctx)
	}()
	s.engine.WS.WaitReady(ctx)

	// 4. Wire WS streams to stores (auto-routes ticker/depth/kline).
	s.stores.WireWS(s.engine.WS, s.engine.Adapter)
	s.wirePersonalWS()

	if s.engine.Adapter != nil {
		if err := s.engine.Adapter.SubscribePersonal(ctx); err != nil {
			s.log.Warn("⚠️ Failed to subscribe personal channels", slog.Any("error", err))
		}
	}

	s.log.Info("🟢 Funding Reversion Background Services Ready")
	return nil
}

func (s *Sniper) wirePersonalWS() {
	if s.engine == nil || s.engine.WS == nil || s.engine.Adapter == nil || s.orderNotifier == nil {
		return
	}

	s.engine.WS.On("personal.order", func(data []byte) {
		order, err := s.engine.Adapter.ParseOrder(data)
		if err != nil {
			s.log.Warn("🟡 Failed to parse personal order WS", slog.Any("error", err))
			return
		}
		if order != nil {
			s.orderNotifier.PublishOrder(*order)
		}
	})

	s.engine.WS.On("personal.order.deal", func(data []byte) {
		deal, err := s.engine.Adapter.ParseOrderDeal(data)
		if err != nil {
			s.log.Warn("🟡 Failed to parse personal order deal WS", slog.Any("error", err))
			return
		}
		if deal != nil {
			s.orderNotifier.PublishDeal(*deal)
		}
	})

	s.engine.WS.On("personal.track.order", func(data []byte) {
		update, err := s.engine.Adapter.ParseTrackOrder(data)
		if err != nil {
			s.log.Warn("🟡 Failed to parse personal track order WS", slog.Any("error", err))
			return
		}
		if update != nil {
			s.orderNotifier.PublishTrackOrder(*update)
		}
	})

	s.engine.WS.On("personal.position", func(data []byte) {
		update, err := s.engine.Adapter.ParsePosition(data)
		if err != nil {
			s.log.Warn("🟡 Failed to parse personal position WS", slog.Any("error", err))
			return
		}
		if update != nil {
			s.orderNotifier.PublishPosition(*update)
		}
	})
}

// Run starts all symbol workers. Blocks until all stop or context is cancelled.
func (s *Sniper) Run(ctx context.Context) error {
	s.log.Info("🚀 Sniper — launching per-symbol workers", slog.Int("symbols", len(s.cfg.Symbols)))

	g, workerCtx := errgroup.WithContext(ctx)
	for i := range s.cfg.Symbols {
		g.Go(s.spawnWorker(workerCtx, s.cfg.Symbols[i]))
	}

	err := g.Wait()
	s.log.Info("🛑 All workers stopped")
	return err
}

// Stop implements the app.Bot interface. It executes any explicit teardown.
// The primary graceful shutdown is handled by the context passed to Run().
func (s *Sniper) Stop(_ context.Context) error {
	s.log.Info("🛑 Sniper explicit stop invoked")
	s.bgWg.Wait()
	return nil
}

// spawnWorker creates a closure that runs one complete cycle for a symbol.
// This replaces the former symbolWorker struct — the orchestrator now
// handles all cycle logic directly.
func (s *Sniper) spawnWorker(ctx context.Context, symCfg config.SymbolConfig) func() error {
	return func() error {
		log := s.log.With("sym", symCfg.Symbol)
		log.Info("🚀 Worker started")
		defer log.Info("🛑 Worker stopped")

		if reason, disabled := s.disabledReason(symCfg.Symbol); disabled {
			log.Warn("🔴 Symbol disabled in-memory", slog.String("reason", reason))
			return nil
		}

		settle, err := GetNextSettleTime(ctx, symCfg.SimulateSettle, symCfg.Symbol, s.stores.Funding())
		if err != nil {
			log.Error("🔴 No settle time", slog.Any("error", err))
			return nil
		}

		// Wait until T-5m before actively entering the cycle.
		if d := s.timeSync.Until(settle.Add(-5 * time.Minute)); d > 0 {
			log.Debug("😴 Waiting for cycle window", slog.Time("settle", settle), slog.Duration("wait", d))
			if err := s.timeSync.Sleep(ctx, d); err != nil {
				return nil
			}
		}

		// If we are already past the firing deadline (T-5s), skip.
		if s.timeSync.Until(settle.Add(-5*time.Second)) <= 0 {
			log.Warn("🔴 Settle time passed or missed", slog.Time("settle", settle))
			return nil
		}

		// Execute one funding cycle via event-driven orchestrator.
		deps := Deps{
			Client:        s.client,
			WsSub:         s.ws,
			OrderNotifier: s.orderNotifier,
			TickerStore:   s.stores.Ticker(),
			ContractStore: s.stores.Contract(),
			PriceStore:    s.stores.Price(),
			FundingStore:  s.stores.Funding(),
			DepthStore:    s.stores.Depth(),
			Clock:         s.timeSync,
			Log:           log,
		}
		orchestrator := NewCycleOrchestrator(symCfg, s.cfg, deps)
		orchestrator.Run(ctx, settle)
		return nil
	}
}

func (s *Sniper) disabledReason(symbol string) (string, bool) {
	s.disabledMu.RLock()
	defer s.disabledMu.RUnlock()
	reason, ok := s.disabled[symbol]
	return reason, ok
}
